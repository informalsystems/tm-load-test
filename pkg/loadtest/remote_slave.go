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

// remoteSlave encapsulates the logic and transport-layer interaction between
// the master and a slave, from the master's perspective. It abstracts the
// low-level transport-layer complexities of interaction into a simple
// interface.
type remoteSlave struct {
	master *Master       // To be able to interact with the master.
	sock   *simpleSocket // The simpler interface to our websocket connection.

	// Remote slave state
	mtx           sync.RWMutex
	id            string
	txCount       int
	state         slaveState
	logger        logging.Logger
	stateMetric   prometheus.Gauge // A numeric representation of the state variable.
	txCountMetric prometheus.Gauge // A way for us to expose the txCount variable via Prometheus.

	stateCtrl chan remoteSlaveStateCtrlMsg
	stop      chan struct{}
	stopped   chan struct{}
}

var slaveStateMetricValues = map[slaveState]float64{
	slaveConnected: 0,
	slaveAccepted:  1,
	slaveRejected:  2,
	slaveTesting:   3,
	slaveFailed:    4,
	slaveCompleted: 5,
}

type remoteSlaveStateCtrlMsg struct {
	newState slaveState
	err      string
	resp     chan error
}

func newRemoteSlave(conn *websocket.Conn, master *Master) *remoteSlave {
	rs := &remoteSlave{
		master: master,
		sock: newSimpleSocket(
			conn,
			ssInboundBufSize(master.expectedSlaves()),
			ssOutboundBufSize(master.expectedSlaves()),
			ssFlushOnStop(true),
			ssSendCloseMessage(false),
			ssParentCtx("remoteSlave"),
		),
		logger:    logging.NewNoopLogger(),
		state:     slaveConnected,
		stateCtrl: make(chan remoteSlaveStateCtrlMsg, 3),
		stop:      make(chan struct{}, 1),
		stopped:   make(chan struct{}, 1),
	}
	return rs
}

func (rs *remoteSlave) Run() {
	go rs.eventLoop()
	// we want the socket operations to run in the foreground, so the remote
	// slave terminates when the socket does
	rs.sock.Run()
}

// StartLoadTest will tell the slave to start its testing process.
func (rs *remoteSlave) StartLoadTest() error {
	return rs.sendCtrlMsg(slaveTesting)
}

// Fail can be called outside of the goroutine that's running the Run method to
// trigger a failure in the remote slave and shut down the local connection. It
// returns any error that may have occurred in communicating the state change
// to the slave.
func (rs *remoteSlave) Fail(err string) error {
	// if the slave's already failed, nothing to do
	if rs.getState() == slaveFailed {
		return nil
	}
	return rs.sendCtrlMsg(slaveFailed, err)
}

// Stop will trigger a shutdown notification for this remote slave's event loop.
func (rs *remoteSlave) Stop() {
	rs.logger.Debug("Stopping remote slave")
	close(rs.stop)
}

func (rs *remoteSlave) eventLoop() {
	var err error

	defer func() {
		rs.sock.Stop()
		close(rs.stopped)
		rs.logger.Debug("Remote slave event loop shut down")
		rs.master.UnregisterRemoteSlave(rs.ID(), err)
	}()

	// the first thing we need to do is get the slave's ID
	if err = rs.readID(); err != nil {
		_ = rs.sock.WriteSlaveMsg(slaveMsg{State: slaveFailed, Error: err.Error()})
		return
	}

	// ask the master to register this slave
	if err = rs.registerRemoteSlave(); err != nil {
		_ = rs.sock.WriteSlaveMsg(slaveMsg{State: slaveRejected, Error: err.Error()})
		return
	}

	// We only now know what the remote slave's ID is, so now we can create its
	// metrics.
	rs.createMetrics()

	// wait until the master indicates that the load test can start, or fail
	if err = rs.waitForStart(); err != nil {
		rs.logger.Error("Failed while waiting for load test to start", "err", err)
		return
	}

	// receive updates from the slave
	if err = rs.receiveTestingUpdates(); err != nil {
		rs.logger.Error("Failed while receiving testing updates from slave", "err", err)
		return
	}

	rs.logger.Info("Remote slave completed testing")
}

// Attempts to obtain the remote slave's ID.
func (rs *remoteSlave) readID() error {
	msg, err := rs.sock.ReadSlaveMsg()
	if err != nil {
		return err
	}
	if len(msg.ID) == 0 {
		return fmt.Errorf("expected non-nil ID for new slave")
	}
	rs.setID(msg.ID)
	rs.logger.Info("Slave connected")
	return nil
}

func (rs *remoteSlave) registerRemoteSlave() error {
	rs.logger.Debug("Attempting to register with master")
	if err := rs.master.RegisterRemoteSlave(rs); err != nil {
		return err
	}
	cfg := rs.master.config()
	// tell the slave it's been accepted and give it its configuration
	return rs.sock.WriteSlaveMsg(slaveMsg{
		ID:     rs.id,
		State:  slaveAccepted,
		Config: &cfg,
	})
}

func (rs *remoteSlave) waitForStart() error {
	rs.logger.Debug("Waiting for load test to start")
	for {
		select {
		case msg := <-rs.stateCtrl:
			msg.resp <- rs.sock.WriteSlaveMsg(slaveMsg{ID: rs.id, State: msg.newState, Error: msg.err})
			if msg.newState == slaveTesting {
				return nil
			}
			return fmt.Errorf("expected next slave state to be \"%s\", but was \"%s\"", slaveTesting, msg.newState)

		case <-rs.stop:
			return fmt.Errorf("wait cancelled")
		}
	}
}

func (rs *remoteSlave) receiveTestingUpdates() error {
	rs.logger.Debug("Receiving load testing updates")
	updateTicker := time.NewTicker(slaveUpdateInterval)
	defer updateTicker.Stop()
	for {
		select {
		case msg := <-rs.stateCtrl:
			if msg.newState == slaveFailed {
				msg.resp <- rs.sock.WriteSlaveMsg(slaveMsg{ID: rs.id, State: msg.newState, Error: msg.err})
				return fmt.Errorf("slave failed: %s", msg.err)
			}

		case <-updateTicker.C:
			rs.logger.Debug("Attempting to receive update from remote slave")
			msg, err := rs.sock.ReadSlaveMsg(slaveUpdateInterval)
			if err != nil {
				return fmt.Errorf("failed to read from remote slave: %s", err.Error())
			}
			if msg.State == slaveFailed {
				return fmt.Errorf("remote slave failed: %s", msg.Error)
			}
			rs.setTxCount(msg.TxCount)
			rs.master.ReceiveSlaveUpdate(msg)
			if msg.State == slaveCompleted {
				rs.stateMetric.Set(slaveStateMetricValues[slaveCompleted])
				return nil
			}

		case <-rs.stop:
			rs.logger.Debug("Got update receiver cancellation notification")
			return fmt.Errorf("update receiver cancelled")
		}
	}
}

// Blocking send operation
func (rs *remoteSlave) sendCtrlMsg(newState slaveState, errors ...string) error {
	rs.logger.Debug("Sending control message", "newState", newState)
	err := ""
	if len(errors) > 0 {
		err = errors[0]
	}
	resp := make(chan error, 1)
	rs.stateCtrl <- remoteSlaveStateCtrlMsg{
		newState: newState,
		err:      err,
		resp:     resp,
	}
	rs.logger.Debug("Waiting for response from control message sent")
	select {
	case resultErr := <-resp:
		if resultErr == nil {
			rs.setState(newState)
			rs.stateMetric.Set(slaveStateMetricValues[newState])
		}
		rs.logger.Debug("Got response", "resultErr", resultErr)
		return resultErr

	case <-time.After(10 * time.Second):
		return fmt.Errorf("timed out waiting for response for control message")
	}
}

func (rs *remoteSlave) setID(id string) {
	rs.mtx.Lock()
	rs.id = id
	rs.logger = logging.NewLogrusLogger(fmt.Sprintf("remoteSlave[%s]", id))
	rs.mtx.Unlock()
}

func (rs *remoteSlave) ID() string {
	rs.mtx.RLock()
	defer rs.mtx.RUnlock()
	return rs.id
}

func (rs *remoteSlave) setTxCount(txCount int) {
	rs.mtx.Lock()
	rs.txCount = txCount
	rs.txCountMetric.Set(float64(txCount))
	rs.mtx.Unlock()
}

func (rs *remoteSlave) TxCount() int {
	rs.mtx.RLock()
	defer rs.mtx.RUnlock()
	return rs.txCount
}

func (rs *remoteSlave) getState() slaveState {
	rs.mtx.RLock()
	defer rs.mtx.RUnlock()
	return rs.state
}

func (rs *remoteSlave) setState(newState slaveState) {
	rs.mtx.Lock()
	rs.state = newState
	if rs.stateMetric != nil {
		rs.stateMetric.Set(slaveStateMetricValues[newState])
	}
	rs.mtx.Unlock()
	rs.logger.Debug("Slave state set", "newState", newState)
}

func (rs *remoteSlave) createMetrics() {
	rs.mtx.Lock()
	rs.stateMetric = promauto.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("tmloadtest_slave_%s_state", rs.id),
		Help: fmt.Sprintf("The current state of slave %s", rs.id),
	})
	rs.stateMetric.Set(slaveStateMetricValues[slaveAccepted])

	rs.txCountMetric = promauto.NewGauge(prometheus.GaugeOpts{
		Name: fmt.Sprintf("tmloadtest_slave_%s_total_txs", rs.id),
		Help: fmt.Sprintf("The total number of transactions sent by slave %s", rs.id),
	})
	rs.mtx.Unlock()
}
