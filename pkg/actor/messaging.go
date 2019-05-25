package actor

// Message is what is passed to/from actors in order to communicate with them.
type Message interface {
	// Sender provides a reference to the actor who sent this message.
	Sender() *ActorRef
	// Content provides the underlying content of the message.
	Content() interface{}
}

// PoisonPill is a message that, when sent to an actor, allows us to stop that
// actor gracefully.
type PoisonPill struct{}

type message struct {
	sender  *ActorRef
	content interface{}
}

func (m message) Sender() *ActorRef {
	return m.sender
}

func (m message) Content() interface{} {
	return m.content
}

// NewMessage allows one to construct a very simple message to send to an actor.
func NewMessage(sender *ActorRef, content interface{}) Message {
	return message{
		sender:  sender,
		content: content,
	}
}
