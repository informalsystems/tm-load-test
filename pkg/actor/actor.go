package actor

// Actor allows us to implement a simple [actor
// model](https://en.wikipedia.org/wiki/Actor_model).
//
// Note that any messages that the actor does not recognise will be completely
// ignored.
type Actor interface {
	// OnStart is called as the actor starts up.
	OnStart() error

	// OnStop is called just before the actor itself shuts down.
	OnStop() error

	// OnReceive is called when new messageChan come through for this actor.
	OnReceive(msg Message)

	// GetID retrieves the UUID of this actor.
	GetID() string
}

// Factory must be defined for a particular actor to be able to generate new
// actors of that type. The given ID must be the UUID of the new actor to be
// created.
type Factory func(id string) Actor

// Props allows us to provide actor-specific properties overrides when
// instantiating an actor.
type Props func(a Actor)

// Start instantiates a single actor using the given factory and options and
// spawns its entire lifecycle in a separate goroutine.
func Start(f Factory, props Props, id string, parent *ActorRef, opts ...Option) *ActorRef {
	ref := newActorRef(id, parent, opts...)
	go eventLoop(f, props, id, ref)
	return ref
}

func eventLoop(f Factory, props Props, id string, ref *ActorRef) {
	// we create the actor within this goroutine
	a := f(id)
	if props != nil {
		props(a)
	}
	ref.lifecycleEvent(LifecycleEvent{Type: Starting})
	if err := a.OnStart(); err != nil {
		ref.lifecycleEvent(LifecycleEvent{
			Sender:  ref,
			Type:    Failed,
			Details: "OnStart() call failed",
			Error:   err,
		})
		return
	}
	ref.lifecycleEvent(LifecycleEvent{Type: Running})
loop:
	for msg := range ref.messageChan.Raw() {
		switch m := msg.(type) {
		case Message:
			switch m.Content().(type) {
			case PoisonPill:
				break loop

			default:
				a.OnReceive(m)
			}
		}
	}
	ref.lifecycleEvent(LifecycleEvent{Type: Stopping})
	if err := a.OnStop(); err != nil {
		ref.lifecycleEvent(LifecycleEvent{
			Sender:  ref,
			Type:    Failed,
			Details: "OnStop() call failed",
			Error:   err,
		})
	}
	ref.lifecycleEvent(LifecycleEvent{Type: Stopped})
}
