package smartchannel

import (
	"fmt"
	"time"
)

type (
	ErrTimedOut struct {
		Timeout time.Duration
	}

	ErrEmpty struct {}

	ErrOverflow struct {
		MaxCapacity int
	}

	ErrClosed struct{}
)

func (e ErrTimedOut) Error() string {
	return fmt.Sprintf("receive timed out waiting after %s", e.Timeout.String())
}

func (e ErrEmpty) Error() string {
	return "channel is empty"
}

func (e ErrOverflow) Error() string {
	return fmt.Sprintf("channel maximum capacity (%d) reached", e.MaxCapacity)
}

func (e ErrClosed) Error() string {
	return "channel is closed"
}
