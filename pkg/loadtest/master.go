package loadtest

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/informalsystems/tm-load-test/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const masterShutdownTimeout = 10 * time.Second

// Master status gauge values
const (
	masterStarting          = 0
	masterWaitingForPeers   = 1
	masterWaitingForWorkers = 2
	masterTesting           = 3
	masterFailed            = 4
	masterCompleted         = 5
)

// The rate at which the master logs progress and updates the Prometheus metrics
// it's writing out.
const masterProgressUpdateInterval = 5 * time.Second

// Master is a WebSockets server that allows workers to connect to it to obtain
// configuration information. It does nothing but coordinate load testing
// amongst the workers.
type Master struct {
	cfg       *Config
	masterCfg *MasterConfig
	logger    logging.Logger

	svr        *http.Server  // The HTTP/WebSockets server.
	svrStopped chan struct{} // Closed when the WebSockets server has shut down.

	workers map[string]*remoteWorker // Registered remote workers.

	workerRegister   chan remoteWorkerRegisterRequest   // Send a request here to register a remote worker.
	workerUnregister chan remoteWorkerUnregisterRequest // Send a request here to unregister a remote worker.
	workerUpdate     chan workerMsg
	stop             chan struct{}

	// Rudimentary statistics
	startTime           time.Time
	lastProgressUpdate  time.Time
	totalTxs            int              // The last calculated total number of transactions across all workers.
	totalBytes          int64            // The last calculated total number of bytes in transactions sent across all workers.
	totalTxsPerWorker   map[string]int   // The number of transactions sent by each worker.
	totalBytesPerWorker map[string]int64 // The total cumulative number of transaction bytes sent by each worker.

	// Prometheus metrics
	stateMetric            prometheus.Gauge // A code-based status metric for representing the master's current state.
	totalTxsMetric         prometheus.Gauge // The total number of transactions sent by all workers.
	totalBytesMetric       prometheus.Gauge // The total cumulative bytes in transactions sent by all workers.
	txRateMetric           prometheus.Gauge // The transaction throughput rate (tx/sec) as measured by the master since the last metrics update.
	txDataRateMetric       prometheus.Gauge // The total transaction throughput rate in bytes/sec as measured by the master.
	overallTxRateMetric    prometheus.Gauge // The overall transaction throughput rate (tx/sec) as measured by the master since the beginning of the load test.
	workersCompletedMetric prometheus.Gauge // The total number of workers that have completed their testing.
	testUnderwayMetric     prometheus.Gauge // The ID of the load test currently underway (-1 if none).

	mtx       sync.Mutex
	cancelled bool
}

type remoteWorkerRegisterRequest struct {
	rw   *remoteWorker
	resp chan error
}

type remoteWorkerUnregisterRequest struct {
	id  string // The ID of the worker to unregister.
	err error  // If any error occurred during the worker's life cycle.
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func NewMaster(cfg *Config, masterCfg *MasterConfig) *Master {
	logger := logging.NewLogrusLogger("master")
	master := &Master{
		cfg:                 cfg,
		masterCfg:           masterCfg,
		logger:              logger,
		svrStopped:          make(chan struct{}, 1),
		workers:             make(map[string]*remoteWorker),
		workerRegister:      make(chan remoteWorkerRegisterRequest, masterCfg.ExpectWorkers),
		workerUnregister:    make(chan remoteWorkerUnregisterRequest, masterCfg.ExpectWorkers),
		workerUpdate:        make(chan workerMsg, masterCfg.ExpectWorkers),
		stop:                make(chan struct{}, 1),
		totalTxsPerWorker:   make(map[string]int),
		totalBytesPerWorker: make(map[string]int64),
		stateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_state",
			Help: "The current state of the tm-load-test master",
		}),
		totalTxsMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_total_txs",
			Help: "The total cumulative number of transactions sent by all workers",
		}),
		totalBytesMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_total_bytes",
			Help: "The total cumulative number of bytes of transactions sent by all workers",
		}),
		txRateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_tx_rate",
			Help: "The current transaction throughput rate (in txs/sec) as seen by the tm-load-test master, summed across all workers",
		}),
		txDataRateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_tx_data_rate",
			Help: "The current transaction throughput rate (in bytes/sec) as seen by the tm-load-test master, summed across all workers",
		}),
		overallTxRateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_overall_tx_rate",
			Help: "The overall transaction throughput rate as seen by the tm-load-test master since the beginning of the load test",
		}),
		workersCompletedMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_workers_completed",
			Help: "The total number of workers that have completed their testing so far",
		}),
		testUnderwayMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_master_test_underway",
			Help: "The ID of the load test currently underway (-1 if none)",
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
	master.testUnderwayMetric.Set(-1)
	return master
}

// Run will execute the master's operations in a blocking manner, returning
// any error that causes one of the workers or the master to fail.
func (m *Master) Run() error {
	// if we care about how many peers are connected in the network, wait
	// for a minimum number of them to connect before even listening for
	// incoming worker connections
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

	if err := m.waitForWorkers(); err != nil {
		m.failAllRemoteWorkers(err.Error())
		m.stateMetric.Set(masterFailed)
		return err
	}

	if err := m.receiveTestingUpdates(); err != nil {
		m.failAllRemoteWorkers(err.Error())
		m.stateMetric.Set(masterFailed)
		return err
	}

	m.stateMetric.Set(masterCompleted)
	return nil
}

func (m *Master) waitForWorkers() error {
	m.logger.Info("Waiting for all workers to connect and register")
	m.stateMetric.Set(masterWaitingForWorkers)

	timeoutTicker := time.NewTicker(time.Duration(m.masterCfg.WorkerConnectTimeout) * time.Second)
	defer timeoutTicker.Stop()

	for {
		select {
		case req := <-m.workerRegister:
			req.resp <- m.registerRemoteWorker(req.rw)
			if len(m.workers) >= m.masterCfg.ExpectWorkers {
				return m.startLoadTest()
			}

		case req := <-m.workerUnregister:
			// we can do this safely during this waiting period without
			// jeopardizing the load testing
			m.unregisterRemoteWorker(req.id)

		case <-timeoutTicker.C:
			return fmt.Errorf("timed out waiting for all workers to connect")

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
	m.logger.Info("Watching for worker updates")
	m.stateMetric.Set(masterTesting)

	// we set the current test underway ID to our configured load test ID for
	// the duration of the test
	m.testUnderwayMetric.Set(float64(m.masterCfg.LoadTestID))
	// and we set it to -1 the moment all workers are done
	defer m.testUnderwayMetric.Set(-1)

	completed := 0

	progressTicker := time.NewTicker(masterProgressUpdateInterval)
	defer progressTicker.Stop()

	m.startTime = time.Now()
	m.lastProgressUpdate = m.startTime

	for {
		select {
		case msg := <-m.workerUpdate:
			m.logger.Debug("Got update from worker", "msg", msg)
			if _, exists := m.workers[msg.ID]; !exists {
				m.logger.Error("Got message from unregistered worker - ignoring", "id", msg.ID)
				continue
			}
			// keep track of how many transactions this worker has reported
			if msg.TxCount > 0 {
				m.totalTxsPerWorker[msg.ID] = msg.TxCount
			}
			// keep track of how many bytes this worker reported
			if msg.TotalTxBytes > 0 {
				m.totalBytesPerWorker[msg.ID] = msg.TotalTxBytes
			}

			switch msg.State {
			case workerTesting:
				m.logger.Debug("Update from remote worker", "id", msg.ID, "txCount", msg.TxCount)

			case workerCompleted:
				m.logger.Debug("Worker completed its testing", "id", msg.ID)
				completed++
				if completed >= m.masterCfg.ExpectWorkers {
					m.logger.Info("All workers completed their load testing")
					m.logTestingProgress(completed)
					return nil
				}

			case workerFailed:
				return fmt.Errorf(msg.Error)

			default:
				return fmt.Errorf("unexpected state from remote worker: %s", msg.State)
			}

		case req := <-m.workerUnregister:
			m.unregisterRemoteWorker(req.id)
			if req.err != nil {
				return fmt.Errorf("remote worker failed: %s", req.err.Error())
			}

		case <-progressTicker.C:
			m.logTestingProgress(completed)

		case <-m.stop:
			m.logger.Debug("Load testing cancel signal received")
			return fmt.Errorf("load testing cancelled")

		case <-m.svrStopped:
			return fmt.Errorf("web server stopped unexpectedly")
		}
	}
}

func (m *Master) RegisterRemoteWorker(rw *remoteWorker) error {
	m.logger.Debug("Attempting to register remote worker")
	resp := make(chan error, 1)
	m.workerRegister <- remoteWorkerRegisterRequest{
		rw:   rw,
		resp: resp,
	}
	select {
	case err := <-resp:
		return err

	case <-time.After(10 * time.Second):
		return fmt.Errorf("timed out while attempting to register remote worker")
	}
}

func (m *Master) registerRemoteWorker(rw *remoteWorker) error {
	m.logger.Debug("Attempting to register remote worker", "id", rw.ID())
	if len(m.workers) >= m.masterCfg.ExpectWorkers {
		return fmt.Errorf("too many workers")
	}
	id := rw.ID()
	if _, exists := m.workers[id]; exists {
		return fmt.Errorf("worker with ID %s already exists", id)
	}
	m.workers[id] = rw
	m.totalTxsPerWorker[id] = 0
	m.totalBytesPerWorker[id] = 0
	m.logger.Info("Added remote worker", "id", id)
	return nil
}

func (m *Master) UnregisterRemoteWorker(id string, err error) {
	m.workerUnregister <- remoteWorkerUnregisterRequest{id: id, err: err}
}

func (m *Master) unregisterRemoteWorker(id string) {
	delete(m.workers, id)
	m.logger.Info("Unregistered worker", "id", id)
}

func (m *Master) ReceiveWorkerUpdate(msg workerMsg) {
	m.workerUpdate <- msg
}

func (m *Master) logTestingProgress(completed int) {
	totalTxs := 0
	for _, txCount := range m.totalTxsPerWorker {
		totalTxs += txCount
	}
	totalBytes := int64(0)
	for _, txBytes := range m.totalBytesPerWorker {
		totalBytes += txBytes
	}
	overallElapsed := time.Since(m.startTime).Seconds()
	elapsed := time.Since(m.lastProgressUpdate).Seconds()

	overallAvgRate := float64(0)
	avgRate := float64(0)
	avgDataRate := float64(0)

	if overallElapsed > 0 {
		overallAvgRate = float64(totalTxs) / overallElapsed
	}
	if elapsed > 0 {
		avgRate = float64(totalTxs-m.totalTxs) / elapsed
		avgDataRate = float64(totalBytes-m.totalBytes) / elapsed
	}

	m.logger.Info(
		"Progress",
		"totalTxs", totalTxs,
		"overallAvgRate", fmt.Sprintf("%.2f txs/sec", overallAvgRate),
		"avgRate", fmt.Sprintf("%.2f txs/sec", avgRate),
		"totalBytes", totalBytes,
	)

	m.lastProgressUpdate = time.Now()
	m.totalTxs = totalTxs
	m.totalBytes = totalBytes
	m.totalTxsMetric.Set(float64(totalTxs))
	m.totalBytesMetric.Set(float64(totalBytes))
	m.txRateMetric.Set(avgRate)
	m.txDataRateMetric.Set(avgDataRate)
	m.overallTxRateMetric.Set(overallAvgRate)
	m.workersCompletedMetric.Set(float64(completed))

	// if we're done and we need to write aggregate statistics
	if completed >= m.masterCfg.ExpectWorkers && len(m.cfg.StatsOutputFile) > 0 {
		stats := AggregateStats{
			TotalTxs:         totalTxs,
			TotalTimeSeconds: overallElapsed,
			TotalBytes:       totalBytes,
		}
		if err := writeAggregateStats(m.cfg.StatsOutputFile, stats); err != nil {
			m.logger.Error("Failed to write aggregate statistics", "err", err)
		}
	}
}

func (m *Master) startLoadTest() error {
	m.logger.Info("All workers connected - starting load test", "count", len(m.workers))
	for id, rw := range m.workers {
		if err := rw.StartLoadTest(); err != nil {
			m.logger.Info("Failed to start load test for worker", "id", id, "err", err)
			return err
		}
	}
	return nil
}

func (m *Master) failAllRemoteWorkers(reason string) {
	m.logger.Debug("Failing all remote workers", "reason", reason)
	for _, rw := range m.workers {
		_ = rw.Fail(reason)
	}
	m.logger.Debug("Failed all remote workers")
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
		newRemoteWorker(conn, m).Run()
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
		cancelTrap := trapInterrupts(func() { close(cancelSleep) }, m.logger)
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

func (m *Master) expectedWorkers() int {
	return m.masterCfg.ExpectWorkers
}

func (m *Master) config() Config {
	return *m.cfg
}

func (m *Master) stopRemoteWorkers() {
	m.logger.Debug("Stopping all remote workers")
	for _, rw := range m.workers {
		rw.Stop()
	}
}

func (m *Master) gracefulShutdown() {
	// stop all remote worker event loops
	m.stopRemoteWorkers()
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
