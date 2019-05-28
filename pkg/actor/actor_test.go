package actor_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/interchainio/tm-load-test/pkg/actor"
	"github.com/interchainio/tm-load-test/pkg/smartchannel"
	"github.com/stretchr/testify/require"
)

type testActor struct {
	id         string
	onStartErr error
	onStopErr  error
	recvChan   chan actor.Message
}

type testError struct{}

var _ actor.Actor = (*testActor)(nil)
var _ error = testError{}

func TestBasicActorLifecycle(t *testing.T) {
	lifecycleChan := smartchannel.New(smartchannel.MaxCapacity(5))
	ref := actor.Start(
		testActorFactory,
		testActorProps(nil, nil, nil),
		"test-actor",
		nil,
		actor.LifecycleEventsChannel(lifecycleChan),
	)
	expectedEvents := []actor.LifecycleEvent{
		actor.LifecycleEvent{
			Type: actor.Starting,
		},
		actor.LifecycleEvent{
			Type: actor.Running,
		},
		actor.LifecycleEvent{
			Type: actor.Stopping,
		},
		actor.LifecycleEvent{
			Type: actor.Stopped,
		},
	}
	for _, expectedEvent := range expectedEvents {
		msg, err := lifecycleChan.Receive(100 * time.Millisecond)
		require.NoError(t, err)
		ev, isLifecycleEvent := msg.(actor.LifecycleEvent)
		require.True(t, isLifecycleEvent, "expected event to be of type actor.LifecycleEvent")
		require.Equal(t, expectedEvent.Type, ev.Type)
		if ev.Type == actor.Running {
			require.NoError(t, ref.Send(actor.NewMessage(nil, actor.PoisonPill{})))
		}
	}
}

func TestActorOnStartFailure(t *testing.T) {
	lifecycleChan := smartchannel.New(smartchannel.MaxCapacity(5))
	_ = actor.Start(
		testActorFactory,
		testActorProps(testError{}, nil, nil),
		"test-actor",
		nil,
		actor.LifecycleEventsChannel(lifecycleChan),
	)
	expectedEvents := []actor.LifecycleEvent{
		actor.LifecycleEvent{
			Type: actor.Starting,
		},
		actor.LifecycleEvent{
			Type:  actor.Failed,
			Error: testError{},
		},
	}
	for _, expectedEvent := range expectedEvents {
		msg, err := lifecycleChan.Receive(100 * time.Millisecond)
		require.NoError(t, err)
		ev, isLifecycleEvent := msg.(actor.LifecycleEvent)
		require.True(t, isLifecycleEvent, "expected event to be of type actor.LifecycleEvent")
		require.Equal(t, expectedEvent.Type, ev.Type)
		if ev.Type == actor.Failed {
			require.Equal(t, expectedEvent.Error, ev.Error)
		}
	}
}

func TestOnStopError(t *testing.T) {
	lifecycleChan := smartchannel.New(smartchannel.MaxCapacity(5))
	ref := actor.Start(
		testActorFactory,
		testActorProps(nil, testError{}, nil),
		"test-actor",
		nil,
		actor.LifecycleEventsChannel(lifecycleChan),
	)
	expectedEvents := []actor.LifecycleEvent{
		actor.LifecycleEvent{
			Type: actor.Starting,
		},
		actor.LifecycleEvent{
			Type: actor.Running,
		},
		actor.LifecycleEvent{
			Type: actor.Stopping,
		},
		actor.LifecycleEvent{
			Type:  actor.Failed,
			Error: testError{},
		},
	}
	for _, expectedEvent := range expectedEvents {
		msg, err := lifecycleChan.Receive(100 * time.Millisecond)
		require.NoError(t, err)
		ev, isLifecycleEvent := msg.(actor.LifecycleEvent)
		require.True(t, isLifecycleEvent, "expected event to be of type actor.LifecycleEvent")
		require.Equal(t, expectedEvent.Type, ev.Type)
		switch ev.Type {
		case actor.Running:
			require.NoError(t, ref.Send(actor.NewMessage(nil, actor.PoisonPill{})))

		case actor.Failed:
			require.Equal(t, expectedEvent.Error, ev.Error)
		}
	}
}

func TestActorReceive(t *testing.T) {
	lifecycleChan := smartchannel.New(smartchannel.MaxCapacity(5))
	recvChan := make(chan actor.Message, 2)
	ref := actor.Start(
		testActorFactory,
		testActorProps(nil, nil, recvChan),
		"test-actor",
		nil,
		actor.LifecycleEventsChannel(lifecycleChan),
	)
	success := false
testLoop:
	for {
		msg, err := lifecycleChan.Receive(100 * time.Millisecond)
		require.NoError(t, err)
		ev, isLifecycleEvent := msg.(actor.LifecycleEvent)
		require.True(t, isLifecycleEvent, "expected event to be of type actor.LifecycleEvent")
		switch ev.Type {
		case actor.Running:
			require.NoError(t, ref.Send(actor.NewMessage(nil, "Hello world!")))
			// wait for the message to come through
			select {
			case m := <-recvChan:
				require.Equal(t, "Hello world!", m.Content())

			case <-time.After(50 * time.Millisecond):
				t.Fatal("Timed out waiting for actor to receive message")
			}
			require.NoError(t, ref.Send(actor.NewMessage(nil, actor.PoisonPill{})))
			success = true

		case actor.Stopped:
			break testLoop
		}
	}
	require.True(t, success, "seems like the test didn't run effectively")
}

func TestActorSupervisionModel(t *testing.T) {
	childCount := 3
	lifecycleChan := smartchannel.New(smartchannel.MaxCapacity(5*childCount))
	parent := actor.Start(
		testActorFactory,
		testActorProps(nil, nil, nil),
		"test-actor-parent",
		nil,
		actor.LifecycleEventsChannel(lifecycleChan),
	)
	children := make([]*actor.ActorRef, 0)
	for i := 0; i < childCount; i++ {
		child := actor.Start(
			testActorFactory,
			testActorProps(nil, nil, nil),
			fmt.Sprintf("test-actor-child-%d", i),
			parent,
			actor.LifecycleEventsChannel(lifecycleChan),
		)
		children = append(children, child)
	}

	errorsc := make(chan error)
	go func() {
		childrenRunning := 0
		childrenStopped := 0
		for rawEvent := range lifecycleChan.Raw() {
			event, ok := rawEvent.(actor.LifecycleEvent)
			if !ok {
				errorsc <- fmt.Errorf("expected event to be of type LifecycleEvent, but was %v", rawEvent)
				return
			}
			// if it's a child actor
			if event.Sender.GetID() != parent.GetID() {
				switch event.Type {
				case actor.Running:
					t.Logf("Child %s up and running", event.Sender.GetID())
					childrenRunning++
					// if all children are running, kill the parent actor - this
					// should trigger the killing of all of the parent's children
					if childrenRunning == childCount {
						t.Log("All children are running - killing parent")
						if err := parent.Send(actor.NewMessage(nil, actor.PoisonPill{})); err != nil {
							errorsc <- err
							return
						}
					}

				case actor.Stopped:
					t.Logf("Child %s stopped", event.Sender.GetID())
					childrenStopped++
					// once all children have stopped, we're done
					if childrenStopped == childCount {
						errorsc <- nil
						return
					}
				}
			}
		}
	}()

	select {
	case err := <-errorsc:
		if err != nil {
			t.Error(err)
		}

	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out waiting for actor supervision tree to terminate")
	}
}

//-----------------------------------------------------------------------------

func testActorProps(onStartErr, onStopErr error, recvChan chan actor.Message) actor.Props {
	return func(a actor.Actor) {
		ta := a.(*testActor)
		ta.onStartErr = onStartErr
		ta.onStopErr = onStopErr
		ta.recvChan = recvChan
	}
}

func testActorFactory(id string) actor.Actor {
	return &testActor{
		id: id,
	}
}

func (a *testActor) OnStart() error {
	return a.onStartErr
}

func (a *testActor) OnStop() error {
	return a.onStopErr
}

func (a *testActor) OnReceive(m actor.Message) {
	if a.recvChan != nil {
		a.recvChan <- m
	}
}

func (a *testActor) GetID() string {
	return a.id
}

func (e testError) Error() string {
	return "test error"
}
