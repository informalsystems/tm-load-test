package actor

type LifecycleEventType string

// The different kinds of lifecycle events that an actor can emit.
const (
	Created  LifecycleEventType = "created"
	Starting LifecycleEventType = "starting"
	Running  LifecycleEventType = "running"
	Stopping LifecycleEventType = "stopping"
	Stopped  LifecycleEventType = "stopped"
	Failed   LifecycleEventType = "failed"
)

// LifecycleEvent objects are emitted from an actor as it undergoes changes to
// its state.
type LifecycleEvent struct {
	Sender  *ActorRef
	Type    LifecycleEventType
	Error   error
	Details string
}
