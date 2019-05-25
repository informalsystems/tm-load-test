package actor

import "github.com/interchainio/tm-load-test/pkg/smartchannel"

// Option overrides particular configuration in the construction of an actor
// and its reference.
type Option func(r *ActorRef)

// MessageChannelCapacity is an option for creating an actor that overrides the
// default message channel capacity.
func MessageChannelCapacity(n int) Option {
	return func(r *ActorRef) {
		r.messageChanCap = n
	}
}

// LifecycleEventsChannel allows one to supply a channel on which to listen
// for lifecycle events from an actor.
func LifecycleEventsChannel(ch *smartchannel.Channel) Option {
	return func(r *ActorRef) {
		r.lifecycleEventsChan = ch
	}
}
