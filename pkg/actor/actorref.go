package actor

import (
	smchan "github.com/interchainio/tm-load-test/pkg/smartchannel"
)

// DefaultMessageChannelCapacity defines the default channel capacity for an
// actor's incoming messages.
const DefaultMessageChannelCapacity = 100

// ActorRef provides a way to communicate with an actor. This object is
// thread-safe and can be passed around.
type ActorRef struct {
	id             string // The UUID of the actor.
	messageChanCap int    // The maximum capacity of the messages channel.

	messageChan         *smchan.Channel // The primary channel by which to communicate with the actor.
	lifecycleEventsChan *smchan.Channel // An optional channel to which to send lifecycle events.
}

func newActorRef(id string, opts ...Option) *ActorRef {
	ref := &ActorRef{
		id:             id,
		messageChanCap: DefaultMessageChannelCapacity,
	}
	for _, opt := range opts {
		opt(ref)
	}
	ref.messageChan = smchan.New(
		smchan.MaxCapacity(ref.messageChanCap),
		smchan.OverflowStrategyFail, // By default, we want an error if the actor's message buffer's full.
	)
	return ref
}

// GetID returns the UUID of the actor to which the reference refers.
func (a *ActorRef) GetID() string {
	return a.id
}

// Send allows us to send a message to the actor behind this actor reference.
// If the actor doesn't have capacity to receive the incoming message (i.e. the
// actor's message buffer is full), this returns an error. This allows for
// intelligent application of back-pressure further up the stack (like perhaps
// exponential back-off).
func (a *ActorRef) Send(msg Message) error {
	return a.messageChan.Send(msg)
}

func (a *ActorRef) lifecycleEvent(e LifecycleEvent) {
	if a.lifecycleEventsChan != nil {
		// fire and forget
		e.Sender = a
		_ = a.lifecycleEventsChan.Send(e)
	}
}
