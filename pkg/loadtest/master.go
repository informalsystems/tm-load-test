package loadtest

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const masterShutdownTimeout = 10 * time.Second

// Master status gauge values
const (
	masterStarting         = 0
	masterWaitingForPeers  = 1
	masterWaitingForSlaves = 2
	masterTesting          = 3
	masterFailed           = 4
	masterCompleted        = 5
)

// The rate at which the master logs progress and updates the Prometheus metrics
// it's writing out.
const masterProgressUpdateInterval = 5 * time.Second

// Master is a WebSockets server that allows slaves to connect to it to obtain
// configuration information. It does nothing but coordinate load testing
// amongst the slaves.
type Master struct {
	cfg       *Config
	masterCfg *MasterConfig
	logger    logging.Logger

	svr        *http.Server  // The HTTP/WebSockets server.
	svrStopped chan struct{} // Closed when the WebSockets server has shut down.

	slaves map[string]*remoteSlave // Registered remote slaves.

	slaveRegister   chan remoteSlaveRegisterRequest   // Send a request here to register a remote slave.
	slaveUnregister chan remoteSlaveUnregisterRequest // Send a request here to unregister a remote slave.
	slaveUpdate     chan slaveMsg
	stop            chan struct{}

	// Rudimentary statistics
	startTime          time.Time
	lastProgressUpdate time.Time
	totalTxs           int            // The last calculated total number of transactions across all slaves.
	totalTxsPerSlave   map[string]int // The number of transactions sent by each slave.

	// Prometheus metrics
	stateMetric         prometheus.Gauge // A code-based status metric for representing the master's current state.
	totalTxsMetric      prometheus.Gauge // The total number of transactions sent by all slaves.
	txRateMetric        prometheus.Gauge // The transaction throughput rate (tx/sec) as measured by the master since the last metrics update.
	overallTxRateMetric prometheus.Gauge // The overall transaction throughput rate (tx/sec) as measured by the master since the beginning of the load test.

	mtx       sync.Mutex
	cancelled bool
}

type remoteSlaveRegisterRequest struct {
	rs   *remoteSlave
	resp chan error
}

type remoteSlaveUnregisterRequest struct {
	id  string // The ID of the slave to unregister.
	err error  // If any error occurred during the slave's life cycle.
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func NewMaster(cfg *Config, masterCfg *MasterConfig) *Master {
	logger := logging.NewLogrusLogger("master")
	master := &Master{
		cfg:              cfg,
		masterCfg:        masterCfg,
		logger:           logger,
		svrStopped:       make(chan struct{}, 1),
		slaves:           make(map[string]*remoteSlave),
		slaveRegister:    make(chan remoteSlaveRegisterRequest, masterCfg.ExpectSlaves),
		slaveUnregister:  make(chan remoteSlaveUnregisterRequest, masterCfg.ExpectSlaves),
		slaveUpdate:      make(chan slaveMsg, masterCfg.ExpectSlaves),
		stop:             make(chan struct{}, 1),
		totalTxsPerSlave: make(map[string]int),
		stateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_state",
			Help: "The current state of the tm-load-test master",
		}),
		totalTxsMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_total_txs",
			Help: "The total cumulative number of transactions sent by all slaves",
		}),
		txRateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_tx_rate",
			Help: "The current transaction throughput rate as seen by the tm-load-test master, summed across all slaves",
		}),
		overallTxRateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_overall_tx_rate",
			Help: "The overall transaction throughput rate as seen by the tm-load-test master since the beginning of the load test",
		}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", master.newWebSocketHandler())
	mux.Handle("/metrics", promhttp.Handler())
	svr := &http.Server{
		Addr:    masterCfg.BindAddr,
		Handler: mux,
	}
	master.svr = svr
	master.stateMetric.Set(masterStarting)
	return master
}

// Run will execute the master's operations in a blocking manner, returning
// any error that causes one of the slaves or the master to fail.
func (m *Master) Run() error {
	// if we care about how many peers are connected in the network, wait
	// for a minimum number of them to connect before even listening for
	// incoming slave connections
	if m.cfg.ExpectPeers > 0 {
		if err := m.waitForPeers(); err != nil {
			m.stateMetric.Set(masterFailed)
			return err
		}
	}

	defer m.gracefulShutdown()

	// we want to know if the user hits Ctrl+Break
	cancelTrap := trapInterrupts(func() {
		m.setCancelled(true)
		close(m.stop)
		m.stateMetric.Set(masterFailed)
	}, m.logger)
	defer func() {
		close(cancelTrap)
	}()

	// we run the WebSockets server in the background
	go m.runServer()

	if err := m.waitForSlaves(); err != nil {
		m.failAllRemoteSlaves(err.Error())
		m.stateMetric.Set(masterFailed)
		return err
	}

	if err := m.receiveTestingUpdates(); err != nil {
		m.failAllRemoteSlaves(err.Error())
		m.stateMetric.Set(masterFailed)
		return err
	}

	m.stateMetric.Set(masterCompleted)
	return nil
}

func (m *Master) waitForSlaves() error {
	m.logger.Info("Waiting for all slaves to connect and register")
	m.stateMetric.Set(masterWaitingForSlaves)

	timeoutTicker := time.NewTicker(time.Duration(m.masterCfg.SlaveConnectTimeout) * time.Second)
	defer timeoutTicker.Stop()

	for {
		select {
		case req := <-m.slaveRegister:
			req.resp <- m.registerRemoteSlave(req.rs)
			if len(m.slaves) >= m.masterCfg.ExpectSlaves {
				return m.startLoadTest()
			}

		case req := <-m.slaveUnregister:
			// we can do this safely during this waiting period without
			// jeopardizing the load testing
			m.unregisterRemoteSlave(req.id)

		case <-timeoutTicker.C:
			return fmt.Errorf("timed out waiting for all slaves to connect")

		case <-m.stop:
			return fmt.Errorf("wait routine cancelled")

		case <-m.svrStopped:
			return fmt.Errorf("web server stopped unexpectedly")
		}
	}
}

// Starts with the configuration file's endpoints list, polling those endpoints
// for unique peers that have connected. Waits until we have the minimum number
// of endpoints, and on success returns a list of peer addresses. On failure,
// returns a relevant error.
func (m *Master) waitForPeers() error {
	m.stateMetric.Set(masterWaitingForPeers)
	peers, err := waitForTendermintNetworkPeers(
		m.cfg.Endpoints,
		m.cfg.EndpointSelectMethod,
		m.cfg.ExpectPeers,
		m.cfg.MinConnectivity,
		m.cfg.MaxEndpoints,
		time.Duration(m.cfg.PeerConnectTimeout)*time.Second,
		m.logger,
	)
	if err != nil {
		m.logger.Error("Failed while waiting for peers to connect", "err", err)
		return err
	}
	m.cfg.Endpoints = peers
	return nil
}

func (m *Master) receiveTestingUpdates() error {
	m.logger.Info("Watching for slave updates")
	m.stateMetric.Set(masterTesting)

	completed := 0

	progressTicker := time.NewTicker(masterProgressUpdateInterval)
	defer progressTicker.Stop()

	m.startTime = time.Now()
	m.lastProgressUpdate = m.startTime

	for {
		select {
		case msg := <-m.slaveUpdate:
			m.logger.Debug("Got update from slave", "msg", msg)
			if _, exists := m.slaves[msg.ID]; !exists {
				m.logger.Error("Got message from unregistered slave - ignoring", "id", msg.ID)
				continue
			}
			// keep track of how many transactions this slave has reported
			if msg.TxCount > 0 {
				m.totalTxsPerSlave[msg.ID] = msg.TxCount
			}

			switch msg.State {
			case slaveTesting:
				m.logger.Debug("Update from remote slave", "id", msg.ID, "txCount", msg.TxCount)

			case slaveCompleted:
				m.logger.Debug("Slave completed its testing", "id", msg.ID)
				completed++
				if completed >= m.masterCfg.ExpectSlaves {
					m.logger.Info("All slaves completed their load testing")
					m.logTestingProgress()
					return nil
				}

			case slaveFailed:
				return fmt.Errorf(msg.Error)

			default:
				return fmt.Errorf("unexpected state from remote slave: %s", msg.State)
			}

		case req := <-m.slaveUnregister:
			m.unregisterRemoteSlave(req.id)
			if req.err != nil {
				return fmt.Errorf("remote slave failed: %s", req.err.Error())
			}

		case <-progressTicker.C:
			m.logTestingProgress()

		case <-m.stop:
			m.logger.Debug("Load testing cancel signal received")
			return fmt.Errorf("load testing cancelled")

		case <-m.svrStopped:
			return fmt.Errorf("web server stopped unexpectedly")
		}
	}
}

func (m *Master) RegisterRemoteSlave(rs *remoteSlave) error {
	m.logger.Debug("Attempting to register remote slave")
	resp := make(chan error, 1)
	m.slaveRegister <- remoteSlaveRegisterRequest{
		rs:   rs,
		resp: resp,
	}
	select {
	case err := <-resp:
		return err

	case <-time.After(10 * time.Second):
		return fmt.Errorf("timed out while attempting to register remote slave")
	}
}

func (m *Master) registerRemoteSlave(rs *remoteSlave) error {
	m.logger.Debug("Attempting to register remote slave", "id", rs.ID())
	if len(m.slaves) >= m.masterCfg.ExpectSlaves {
		return fmt.Errorf("too many slaves")
	}
	id := rs.ID()
	if _, exists := m.slaves[id]; exists {
		return fmt.Errorf("slave with ID %s already exists", id)
	}
	m.slaves[id] = rs
	m.totalTxsPerSlave[id] = 0
	m.logger.Info("Added remote slave", "id", id)
	return nil
}

func (m *Master) UnregisterRemoteSlave(id string, err error) {
	m.slaveUnregister <- remoteSlaveUnregisterRequest{id: id, err: err}
}

func (m *Master) unregisterRemoteSlave(id string) {
	delete(m.slaves, id)
	m.logger.Info("Unregistered slave", "id", id)
}

func (m *Master) ReceiveSlaveUpdate(msg slaveMsg) {
	m.slaveUpdate <- msg
}

func (m *Master) logTestingProgress() {
	totalTxs := 0
	for _, txCount := range m.totalTxsPerSlave {
		totalTxs += txCount
	}
	overallElapsed := time.Since(m.startTime).Seconds()
	elapsed := time.Since(m.lastProgressUpdate).Seconds()

	overallAvgRate := float64(0)
	avgRate := float64(0)

	if overallElapsed > 0 {
		overallAvgRate = float64(totalTxs) / overallElapsed
	}
	if elapsed > 0 {
		avgRate = float64(totalTxs-m.totalTxs) / elapsed
	}

	m.logger.Info(
		"Progress",
		"totalTxs", totalTxs,
		"overallAvgRate", fmt.Sprintf("%.2f txs/sec", overallAvgRate),
		"avgRate", fmt.Sprintf("%.2f txs/sec", avgRate),
	)

	m.lastProgressUpdate = time.Now()
	m.totalTxs = totalTxs
	m.totalTxsMetric.Set(float64(totalTxs))
	m.txRateMetric.Set(avgRate)
	m.overallTxRateMetric.Set(overallAvgRate)
}

func (m *Master) startLoadTest() error {
	m.logger.Info("All slaves connected - starting load test", "count", len(m.slaves))
	for id, rs := range m.slaves {
		if err := rs.StartLoadTest(); err != nil {
			m.logger.Info("Failed to start load test for slave", "id", id, "err", err)
			return err
		}
	}
	return nil
}

func (m *Master) failAllRemoteSlaves(reason string) {
	m.logger.Debug("Failing all remote slaves", "reason", reason)
	for _, rs := range m.slaves {
		_ = rs.Fail(reason)
	}
	m.logger.Debug("Failed all remote slaves")
}

func (m *Master) newWebSocketHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			m.logger.Error("Error while attempting to upgrade incoming WebSockets connection", "err", err)
			return
		}
		defer conn.Close()

		m.logger.Debug("Received incoming WebSockets connection", "r", r)
		newRemoteSlave(conn, m).Run()
	}
}

func (m *Master) runServer() {
	defer close(m.svrStopped)

	m.logger.Info("Starting WebSockets server")

	if err := m.svr.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		m.logger.Error("Server shut down", "err", err)
		return
	}
	m.logger.Info("Server shut down")
}

// Graceful shutdown for the web server.
func (m *Master) shutdownServer() {
	// we only care about the shutdown wait period if we haven't been killed
	if !m.wasCancelled() && m.masterCfg.ShutdownWait > 0 {
		m.logger.Info("Entering post-shutdown wait period", "wait", fmt.Sprintf("%ds", m.masterCfg.ShutdownWait))
		cancelSleep := make(chan struct{})
		cancelTrap := trapInterrupts(func() { close(cancelSleep); }, m.logger)
		select {
		case <-cancelSleep:
			m.logger.Info("Cancelling shutdown wait")
		case <-time.After(time.Duration(m.masterCfg.ShutdownWait) * time.Second):
		}
		close(cancelTrap)
	}
	m.logger.Info("Shutting down web server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.svr.Shutdown(ctx); err != nil {
		m.logger.Error("Failed to gracefully shut down web server", "err", err)
	} else {
		m.logger.Info("Shut down web server")
	}
}

func (m *Master) expectedSlaves() int {
	return m.masterCfg.ExpectSlaves
}

func (m *Master) config() Config {
	return *m.cfg
}

func (m *Master) stopRemoteSlaves() {
	m.logger.Debug("Stopping all remote slaves")
	for _, rs := range m.slaves {
		rs.Stop()
	}
}

func (m *Master) gracefulShutdown() {
	// stop all remote slave event loops
	m.stopRemoteSlaves()
	// gracefully shut down the WebSockets server
	m.shutdownServer()
	select {
	case <-m.svrStopped:
	case <-time.After(masterShutdownTimeout):
		m.logger.Error("Failed to shut down within the required time period")
	}
}

func (m *Master) setCancelled(cancelled bool) {
	m.mtx.Lock()
	m.cancelled = true
	m.mtx.Unlock()
}

func (m *Master) wasCancelled() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.cancelled
}
