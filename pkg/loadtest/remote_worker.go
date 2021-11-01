package loadtest

import (
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/informalsystems/tm-load-test/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// remoteWorker encapsulates the logic and transport-layer interaction between
// the master and a worker, from the master's perspective. It abstracts the
// low-level transport-layer complexities of interaction into a simple
// interface.
type remoteWorker struct {
	master *Master       // To be able to interact with the master.
	sock   *simpleSocket // The simpler interface to our websocket connection.

	// Remote worker state
	mtx           sync.RWMutex
	id            string
	txCount       int
	state         workerState
	logger        logging.Logger
	stateMetric   prometheus.Gauge // A numeric representation of the state variable.
	txCountMetric prometheus.Gauge // A way for us to expose the txCount variable via Prometheus.

	stateCtrl chan remoteWorkerStateCtrlMsg
	stop      chan struct{}
	stopped   chan struct{}
}

var workerStateMetricValues = map[workerState]float64{
	workerConnected: 0,
	workerAccepted:  1,
	workerRejected:  2,
	workerTesting:   3,
	workerFailed:    4,
	workerCompleted: 5,
}

type remoteWorkerStateCtrlMsg struct {
	newState workerState
	err      string
	resp     chan error
}

func newRemoteWorker(conn *websocket.Conn, master *Master) *remoteWorker {
	rs := &remoteWorker{
		master: master,
		sock: newSimpleSocket(
			conn,
			ssInboundBufSize(master.expectedWorkers()),
			ssOutboundBufSize(master.expectedWorkers()),
			ssFlushOnStop(true),
			ssSendCloseMessage(false),
			ssParentCtx("remoteWorker"),
		),
		logger:    logging.NewNoopLogger(),
		state:     workerConnected,
		stateCtrl: make(chan remoteWorkerStateCtrlMsg, 3),
		stop:      make(chan struct{}, 1),
		stopped:   make(chan struct{}, 1),
	}
	return rs
}

func (rw *remoteWorker) Run() {
	go rw.eventLoop()
	// we want the socket operations to run in the foreground, so the remote
	// worker terminates when the socket does
	rw.sock.Run()
}

// StartLoadTest will tell the worker to start its testing process.
func (rw *remoteWorker) StartLoadTest() error {
	return rw.sendCtrlMsg(workerTesting)
}

// Fail can be called outside of the goroutine that's running the Run method to
// trigger a failure in the remote worker and shut down the local connection. It
// returns any error that may have occurred in communicating the state change
// to the worker.
func (rw *remoteWorker) Fail(err string) error {
	// if the worker's already failed, nothing to do
	if rw.getState() == workerFailed {
		return nil
	}
	return rw.sendCtrlMsg(workerFailed, err)
}

// Stop will trigger a shutdown notification for this remote worker's event loop.
func (rw *remoteWorker) Stop() {
	rw.logger.Debug("Stopping remote worker")
	close(rw.stop)
}

func (rw *remoteWorker) eventLoop() {
	var err error

	defer func() {
		rw.sock.Stop()
		close(rw.stopped)
		rw.logger.Debug("Remote worker event loop shut down")
		rw.master.UnregisterRemoteWorker(rw.ID(), err)
	}()

	// the first thing we need to do is get the worker's ID
	if err = rw.readID(); err != nil {
		_ = rw.sock.WriteWorkerMsg(workerMsg{State: workerFailed, Error: err.Error()})
		return
	}

	// ask the master to register this worker
	if err = rw.registerRemoteWorker(); err != nil {
		_ = rw.sock.WriteWorkerMsg(workerMsg{State: workerRejected, Error: err.Error()})
		return
	}

	// We only now know what the remote worker's ID is, so now we can create its
	// metrics.
	rw.createMetrics()

	// wait until the master indicates that the load test can start, or fail
	if err = rw.waitForStart(); err != nil {
		rw.logger.Error("Failed while waiting for load test to start", "err", err)
		return
	}

	// receive updates from the worker
	if err = rw.receiveTestingUpdates(); err != nil {
		rw.logger.Error("Failed while receiving testing updates from worker", "err", err)
		return
	}

	rw.logger.Info("Remote worker completed testing")
}

// Attempts to obtain the remote worker's ID.
func (rw *remoteWorker) readID() error {
	msg, err := rw.sock.ReadWorkerMsg()
	if err != nil {
		return err
	}
	if len(msg.ID) == 0 {
		return fmt.Errorf("expected non-nil ID for new worker")
	}
	rw.setID(msg.ID)
	rw.logger.Info("Worker connected")
	return nil
}

func (rw *remoteWorker) registerRemoteWorker() error {
	rw.logger.Debug("Attempting to register with master")
	if err := rw.master.RegisterRemoteWorker(rw); err != nil {
		return err
	}
	cfg := rw.master.config()
	// tell the worker it's been accepted and give it its configuration
	return rw.sock.WriteWorkerMsg(workerMsg{
		ID:     rw.id,
		State:  workerAccepted,
		Config: &cfg,
	})
}

func (rw *remoteWorker) waitForStart() error {
	rw.logger.Debug("Waiting for load test to start")
	for {
		select {
		case msg := <-rw.stateCtrl:
			msg.resp <- rw.sock.WriteWorkerMsg(workerMsg{ID: rw.id, State: msg.newState, Error: msg.err})
			if msg.newState == workerTesting {
				return nil
			}
			return fmt.Errorf("expected next worker state to be \"%s\", but was \"%s\"", workerTesting, msg.newState)

		case <-rw.stop:
			return fmt.Errorf("wait cancelled")
		}
	}
}

func (rw *remoteWorker) receiveTestingUpdates() error {
	rw.logger.Debug("Receiving load testing updates")
	updateTicker := time.NewTicker(workerUpdateInterval)
	defer updateTicker.Stop()
	for {
		select {
		case msg := <-rw.stateCtrl:
			if msg.newState == workerFailed {
				msg.resp <- rw.sock.WriteWorkerMsg(workerMsg{ID: rw.id, State: msg.newState, Error: msg.err})
				return fmt.Errorf("worker failed: %s", msg.err)
			}

		case <-updateTicker.C:
			rw.logger.Debug("Attempting to receive update from remote worker")
			msg, err := rw.sock.ReadWorkerMsg(workerUpdateInterval)
			if err != nil {
				return fmt.Errorf("failed to read from remote worker: %s", err.Error())
			}
			if msg.State == workerFailed {
				return fmt.Errorf("remote worker failed: %s", msg.Error)
			}
			rw.setTxCount(msg.TxCount)
			rw.master.ReceiveWorkerUpdate(msg)
			if msg.State == workerCompleted {
				rw.stateMetric.Set(workerStateMetricValues[workerCompleted])
				return nil
			}

		case <-rw.stop:
			rw.logger.Debug("Got update receiver cancellation notification")
			return fmt.Errorf("update receiver cancelled")
		}
	}
}

// Blocking send operation
func (rw *remoteWorker) sendCtrlMsg(newState workerState, errors ...string) error {
	rw.logger.Debug("Sending control message", "newState", newState)
	err := ""
	if len(errors) > 0 {
		err = errors[0]
	}
	resp := make(chan error, 1)
	rw.stateCtrl <- remoteWorkerStateCtrlMsg{
		newState: newState,
		err:      err,
		resp:     resp,
	}
	rw.logger.Debug("Waiting for response from control message sent")
	select {
	case resultErr := <-resp:
		if resultErr == nil {
			rw.setState(newState)
			rw.stateMetric.Set(workerStateMetricValues[newState])
		}
		rw.logger.Debug("Got response", "resultErr", resultErr)
		return resultErr

	case <-time.After(10 * time.Second):
		return fmt.Errorf("timed out waiting for response for control message")
	}
}

func (rw *remoteWorker) setID(id string) {
	rw.mtx.Lock()
	rw.id = id
	rw.logger = logging.NewLogrusLogger(fmt.Sprintf("remoteWorker[%s]", id))
	rw.mtx.Unlock()
}

func (rw *remoteWorker) ID() string {
	rw.mtx.RLock()
	defer rw.mtx.RUnlock()
	return rw.id
}

func (rw *remoteWorker) setTxCount(txCount int) {
	rw.mtx.Lock()
	rw.txCount = txCount
	rw.txCountMetric.Set(float64(txCount))
	rw.mtx.Unlock()
}

func (rw *remoteWorker) TxCount() int {
	rw.mtx.RLock()
	defer rw.mtx.RUnlock()
	return rw.txCount
}

func (rw *remoteWorker) getState() workerState {
	rw.mtx.RLock()
	defer rw.mtx.RUnlock()
	return rw.state
}

func (rw *remoteWorker) setState(newState workerState) {
	rw.mtx.Lock()
	rw.state = newState
	if rw.stateMetric != nil {
		rw.stateMetric.Set(workerStateMetricValues[newState])
	}
	rw.mtx.Unlock()
	rw.logger.Debug("Worker state set", "newState", newState)
}

func (rw *remoteWorker) createMetrics() {
	rw.mtx.Lock()
	rw.stateMetric = promauto.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("tmloadtest_worker_%s_state", rw.id),
		Help: fmt.Sprintf("The current state of worker %s", rw.id),
	})
	rw.stateMetric.Set(workerStateMetricValues[workerAccepted])

	rw.txCountMetric = promauto.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("tmloadtest_worker_%s_total_txs", rw.id),
		Help: fmt.Sprintf("The total number of transactions sent by worker %s", rw.id),
	})
	rw.mtx.Unlock()
}
