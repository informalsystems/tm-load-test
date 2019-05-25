package smartchannel

import (
	"fmt"
	"sync"
	"time"
)

// OverflowStrategy allows us to define different kinds of strategies when a
// channel is full to capacity and we receive a new message.
type OverflowStrategy string

// ReadStrategy defines how to handle the case when the channel is empty and a
// read is attempted.
type ReadStrategy string

// Overflow strategies for when a channel is full to its capacity.
const (
	OverflowBlock OverflowStrategy = "block" // Just block until there is capacity (normal Go channel behaviour).
	OverflowFail  OverflowStrategy = "fail"  // Returns an error immediately if the channel is full.
)

// Read strategies for when a channel is empty and a read is attempted.
const (
	ReadBlock ReadStrategy = "block" // Block until a message comes through.
	ReadFail  ReadStrategy = "fail"  // Return an error immediately if the channel's empty.
)

// DefaultMaxCapacity is the default maximum capacity of a channel if one is not
// supplied in the constructor.
const DefaultMaxCapacity = 1

// DefaultOverflowStrategy is the overflow strategy of a channel if one is not
// supplied in the constructor.
const DefaultOverflowStrategy = OverflowBlock

// DefaultReadStrategy is the read strategy of a channel if one is not supplied
// in the constructor.
const DefaultReadStrategy = ReadBlock

// State allows us to keep track of the current state of the channel.
type State string

const (
	Open   State = "open"
	Closed State = "closed"
)

// Channel implements a slightly smarter channel interface than that which Go
// provides natively. All operations should be thread-safe, except for the Raw
// method.
type Channel struct {
	mtx              sync.RWMutex
	ch               chan interface{}
	maxCapacity      int
	readStrategy     ReadStrategy
	overflowStrategy OverflowStrategy
	state            State
}

// Option defines options to modify the channel's default behaviour.
type Option func(ch *Channel)

// ReceiveResult is obtained from asynchronous receive operations.
type ReceiveResult struct {
	Message interface{}
	Error   error
}

func MaxCapacity(n int) Option {
	return func(ch *Channel) {
		ch.maxCapacity = n
	}
}

func OverflowStrategyBlock(ch *Channel) {
	ch.overflowStrategy = OverflowBlock
}

func OverflowStrategyFail(ch *Channel) {
	ch.overflowStrategy = OverflowFail
}

func ReadStrategyBlock(ch *Channel) {
	ch.readStrategy = ReadBlock
}

func ReadStrategyFail(ch *Channel) {
	ch.readStrategy = ReadFail
}

// New creates a smart channel with the given maximum capacity and
// overflow strategy.
func New(opts ...Option) *Channel {
	ch := &Channel{
		maxCapacity:      DefaultMaxCapacity,
		readStrategy:     DefaultReadStrategy,
		overflowStrategy: DefaultOverflowStrategy,
		state:            Open,
	}
	for _, opt := range opts {
		opt(ch)
	}
	ch.ch = make(chan interface{}, ch.maxCapacity)
	return ch
}

// MaxCapacity reports the preconfigured maximum capacity of this channel.
func (c *Channel) MaxCapacity() int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.maxCapacity
}

// ReadStrategy reports the preconfigured read strategy for this channel.
func (c *Channel) ReadStrategy() ReadStrategy {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.readStrategy
}

// OverflowStrategy reports the preconfigured overflow strategy for this
// channel.
func (c *Channel) OverflowStrategy() OverflowStrategy {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.overflowStrategy
}

// State reports the current state of this channel.
func (c *Channel) State() State {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.state
}

// Size returns the number of items buffered in the channel right now. If the
// channel is closed, it will always return 0.
func (c *Channel) Size() int {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	if c.state == Closed {
		return 0
	}
	return len(c.ch)
}

// Raw provides access to the raw underlying Go primitive channel. This is
// useful if you need to be able to select on the channel. There is no error
// checking here as to whether or not the channel is closed. Not thread-safe.
func (c *Channel) Raw() chan interface{} {
	return c.ch
}

// Receive performs a blocking receive operation on the underlying Go channel.
// This will block forever until a message is received, unless the optional
// timeout parameter is supplied.
func (c *Channel) Receive(timeout ...time.Duration) (interface{}, error) {
	if c.State() == Closed {
		return nil, ErrClosed{}
	}
	if c.Size() == 0 {
		switch c.readStrategy {
		case ReadFail:
			return nil, ErrEmpty{}
		}
	}
	if len(timeout) == 0 {
		return <-c.ch, nil
	}
	select {
	case m := <-c.ch:
		return m, nil

	case <-time.After(timeout[0]):
		return nil, ErrTimedOut{Timeout: timeout[0]}
	}
}

// ReceiveAsync does the same as Receive, but asynchronously in a separate
// goroutine. All results are returned via the channel that's returned. Allows
// one to select over the receive operation.
func (c *Channel) ReceiveAsync(timeout ...time.Duration) chan ReceiveResult {
	asyncChan := make(chan ReceiveResult, 1)
	go func() {
		m, err := c.Receive(timeout...)
		asyncChan <- ReceiveResult{Message: m, Error: err}
	}()
	return asyncChan
}

// Send attempts to send the given message according to the configured overflow
// strategy. It also provides an optional timeout parameter for the operation.
func (c *Channel) Send(msg interface{}, timeout ...time.Duration) error {
	// we do a read lock here, which allows for multiple senders and receivers,
	// but doesn't allow the state to change during this operation
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	if c.state == Closed {
		return ErrClosed{}
	}

	if len(c.ch) == c.maxCapacity {
		switch c.overflowStrategy {
		case OverflowFail:
			return ErrOverflow{MaxCapacity: c.maxCapacity}
		}
	}

	if len(timeout) == 0 {
		c.ch <- msg
		return nil
	}
	select {
	case c.ch <- msg:
		return nil

	case <-time.After(timeout[0]):
		return ErrTimedOut{Timeout: timeout[0]}
	}
}

// SendAsync is the same as Send, but does so asynchronously so that one can
// select over the send operation. The send operation occurs in a separate
// goroutine.
func (c *Channel) SendAsync(msg interface{}, timeout ...time.Duration) chan error {
	asyncChan := make(chan error, 1)
	go func() {
		asyncChan <- c.Send(msg, timeout...)
	}()
	return asyncChan
}

// PollSize calls the given callback at the specified frequency to report back
// on how many items are currently in the channel. It returns a channel that,
// when closed, stops the reporting loop. The reporting loop is spawned in a
// separate goroutine.
func (c *Channel) PollSize(d time.Duration, callback func(n int)) (chan struct{}, error) {
	if c.State() == Closed {
		return nil, ErrClosed{}
	}

	closeChan := make(chan struct{})
	go func() {
		ticker := time.NewTicker(d)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// keep checking before trying to access the channel
				if c.State() == Closed {
					return
				}
				callback(c.Size())

			case <-closeChan:
				return
			}
		}
	}()
	return closeChan, nil
}

// Close will attempt to close the channel if it's open.
func (c *Channel) Close() {
	// allow no reads/writes during this operation
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.state == Closed {
		return
	}
	close(c.ch)
	c.state = Closed
}

// Reopen will effectively recreate the underlying Go channel, but keep the same
// original creation parameters for the channel.
func (c *Channel) Reopen() {
	// allow no reads/writes during this operation
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.state == Open {
		return
	}
	c.ch = make(chan interface{}, c.maxCapacity)
	c.state = Open
}

func (c *Channel) String() string {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return fmt.Sprintf(
		"Channel{state=%s, maxCapacity=%d, OverflowStrategy=%s}",
		c.state,
		c.maxCapacity,
		c.overflowStrategy,
	)
}
