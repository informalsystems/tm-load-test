package smartchannel_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/interchainio/tm-load-test/pkg/smartchannel"
	"github.com/stretchr/testify/require"
)

func TestBasicSendAndReceive(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()
	require.NoError(t, ch.Send("hello"))
	m, err := ch.Receive()
	require.NoError(t, err)
	require.Equal(t, "hello", m)
}

func TestGetters(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()
	require.Equal(t, 1, ch.MaxCapacity())
	require.Equal(t, smartchannel.OverflowBlock, ch.OverflowStrategy())
	require.Equal(t, smartchannel.Open, ch.State())
}

func TestRawChannelAccess(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()
	ch.Raw() <- "hello"
	m := <-ch.Raw()
	require.Equal(t, "hello", m)
}

func TestReceiveWithTimeout(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()
	donec := make(chan struct{})
	var v interface{}
	var err error
	go func() {
		defer close(donec)
		// nobody's sent anything, so we should time out
		v, err = ch.Receive(50 * time.Millisecond)
	}()

	select {
	case <-donec:
		require.Nil(t, v)
		_, isTimeout := err.(smartchannel.ErrTimedOut)
		require.True(t, isTimeout, "expected error to be of type ErrTimedOut")

	case <-time.After(60 * time.Millisecond):
		t.Fatal("Receive operation timeout didn't work")
	}
}

func TestReceiveAsync(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()

	require.NoError(t, ch.Send("hello"))

	select {
	case r := <-ch.ReceiveAsync():
		require.NoError(t, r.Error)
		require.Equal(t, "hello", r.Message)

	case <-time.After(50 * time.Millisecond):
		t.Fatal("Receive operation timed out")
	}
}

func TestReceiveAsyncWithTimeout(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()

	select {
	case r := <-ch.ReceiveAsync(50 * time.Millisecond):
		require.Error(t, r.Error)
		_, isTimeout := r.Error.(smartchannel.ErrTimedOut)
		require.True(t, isTimeout, "expected error to be of type ErrTimedOut")

	case <-time.After(60 * time.Millisecond):
		t.Fatal("Receive operation timeout didn't work")
	}
}

func TestSendWithTimeout(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()

	// fill the channel
	require.NoError(t, ch.Send("message 1"))

	donec := make(chan struct{})
	var err error
	go func() {
		defer close(donec)
		// this should block
		err = ch.Send("message 2", 50*time.Millisecond)
	}()

	select {
	case <-donec:
		require.NotNil(t, err)
		_, isTimeout := err.(smartchannel.ErrTimedOut)
		require.True(t, isTimeout, "expected error to be of type ErrTimedOut")

	case <-time.After(60 * time.Millisecond):
		t.Fatal("Send operation timeout didn't work")
	}
}

func TestSendAsync(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()

	select {
	case err := <-ch.SendAsync("hello"):
		require.NoError(t, err)
		r, err := ch.Receive()
		require.NoError(t, err)
		require.Equal(t, "hello", r)

	case <-time.After(50 * time.Millisecond):
		t.Fatal("Send operation timed out")
	}
}

func TestOpenClose(t *testing.T) {
	ch := smartchannel.New()
	defer ch.Close()
	require.Equal(t, smartchannel.Open, ch.State())
	ch.Close()
	require.Equal(t, smartchannel.Closed, ch.State())

	// all of these cases should return the ErrClosed error when the channel is
	// closed
	testCases := []struct{ fn func() error }{
		{
			fn: func() error {
				return ch.Send("shouldn't go through")
			},
		},
		{
			fn: func() error {
				_, err := ch.Receive()
				return err
			},
		},
		{
			fn: func() error {
				_, err := ch.PollSize(10*time.Millisecond, func(n int) {})
				return err
			},
		},
	}

	for _, tc := range testCases {
		err := tc.fn()
		require.Error(t, err)
		_, isClosed := err.(smartchannel.ErrClosed)
		require.True(t, isClosed, "expected error to be of type ErrClosed")
	}

	// should be no messages in there now
	require.Equal(t, 0, ch.Size())

	// now we reopen the channel
	ch.Reopen()
	require.Equal(t, smartchannel.Open, ch.State())
	err := ch.Send("should go through")
	require.NoError(t, err)
	// should have 1 message in there
	require.Equal(t, 1, ch.Size())
	m, err := ch.Receive()
	require.NoError(t, err)
	require.Equal(t, "should go through", m)
}

func TestFailOverflowStrategy(t *testing.T) {
	ch := smartchannel.New(smartchannel.OverflowStrategyFail)
	defer ch.Close()

	err := <-ch.SendAsync("this should go through", 50*time.Millisecond)
	require.NoError(t, err)

	err = <-ch.SendAsync("this should not go through", 50*time.Millisecond)
	require.Error(t, err)
	_, isErrOverflow := err.(smartchannel.ErrOverflow)
	require.True(t, isErrOverflow, "expected error to be of type ErrOverflow")
}

func TestFailReadStrategy(t *testing.T) {
	ch := smartchannel.New(smartchannel.ReadStrategyFail)
	defer ch.Close()

	_, err := ch.Receive()
	require.Error(t, err)
	_, isErrEmpty := err.(smartchannel.ErrEmpty)
	require.True(t, isErrEmpty, "expected error to be of type ErrEmpty")
}

func TestSizePolling(t *testing.T) {
	ch := smartchannel.New(smartchannel.MaxCapacity(3))
	defer ch.Close()

	sizec := make(chan int, 1)
	closec, err := ch.PollSize(50*time.Millisecond, func(n int) {
		sizec <- n
	})
	require.NoError(t, err)
	defer close(closec)

	for size := 0; size < 3; size++ {
		require.NoError(t, ch.Send(fmt.Sprintf("message %d", size)))
		select {
		case n := <-sizec:
			require.Equal(t, size+1, n)

		case <-time.After(60 * time.Millisecond):
			t.Fatal("Timed out")
		}
	}
}
