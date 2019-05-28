package actor

import (
	"sync"

	smchan "github.com/interchainio/tm-load-test/pkg/smartchannel"
)

// DefaultMessageChannelCapacity defines the default channel capacity for an
// actor's incoming messages.
const DefaultMessageChannelCapacity = 100

// ActorRef provides a way to communicate with an actor. This object is
// thread-safe and can be passed around. Do not make copies of this object -
// use only a single pointer to this actor.
type ActorRef struct {
	id             string // The UUID of the actor.
	messageChanCap int    // The maximum capacity of the messages channel.

	messageChan         *smchan.Channel // The primary channel by which to communicate with the actor.
	lifecycleEventsChan *smchan.Channel // An optional channel to which to send lifecycle events.

	// We use a bidirectional mapping here to track the relationships between
	// parent and child actors for a rudimentary supervision tree.
	mtx      sync.RWMutex
	parent   *ActorRef
	children map[string]*ActorRef
	state    LifecycleEventType
}

func newActorRef(id string, parent *ActorRef, opts ...Option) *ActorRef {
	ref := &ActorRef{
		id:             id,
		messageChanCap: DefaultMessageChannelCapacity,
		children:       make(map[string]*ActorRef),
		state:          Creating,
	}
	for _, opt := range opts {
		opt(ref)
	}
	ref.messageChan = smchan.New(
		smchan.MaxCapacity(ref.messageChanCap),
		smchan.OverflowStrategyFail, // By default, we want an error if the actor's message buffer's full.
	)
	// sets this parent, and adds a reference in the parent's `children` map
	ref.setParent(parent)
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
	a.setState(e.Type)
	if a.lifecycleEventsChan != nil {
		// fire and forget
		e.Sender = a
		_ = a.lifecycleEventsChan.Send(e)
	}

	// if we're stopping somehow, stop all children too
	if e.Type == Failed || e.Type == Stopping || e.Type == Stopped {
		// TODO: Decide on a strategy for handling the case that we can't
		//       gracefully stop all children.
		a.stopChildren()
	}
}

func (a *ActorRef) addChild(child *ActorRef) {
	if child == nil {
		return
	}
	a.mtx.Lock()
	defer a.mtx.Unlock()
	a.children[child.id] = child
}

func (a *ActorRef) removeChild(id string) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	delete(a.children, id)
}

func (a *ActorRef) setParent(parent *ActorRef) {
	// if there's an existing parent
	if a.parent != nil {
		// remove this child from its watch
		a.parent.removeChild(a.id)
	}
	if parent != nil {
		// add this actor to the parent's watch
		parent.addChild(a)
	}
	// set this actor's parent
	a.mtx.Lock()
	defer a.mtx.Unlock()
	a.parent = parent
}

func (a *ActorRef) State() LifecycleEventType {
	a.mtx.RLock()
	defer a.mtx.RUnlock()
	return a.state
}

func (a *ActorRef) setState(state LifecycleEventType) {
	a.mtx.Lock()
	defer a.mtx.Unlock()
	a.state = state
}

// This does a best-effort stop attempt for all children of this actor.
func (a *ActorRef) stopChildren() {
	a.mtx.Lock()
	defer a.mtx.Unlock()

	stopped := make([]string, 0)
	for childID, child := range a.children {
		if err := child.Send(NewMessage(a, PoisonPill{})); err == nil {
			stopped = append(stopped, childID)
		}
	}
	// remove only the children we've managed to successfully stop
	for _, childID := range stopped {
		delete(a.children, childID)
	}
}
