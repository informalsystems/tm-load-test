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

const coordShutdownTimeout = 10 * time.Second

// Coordinator status gauge values
const (
	coordStarting          = 0
	coordWaitingForPeers   = 1
	coordWaitingForWorkers = 2
	coordTesting           = 3
	coordFailed            = 4
	coordCompleted         = 5
)

// The rate at which the coordinator logs progress and updates the Prometheus metrics
// it's writing out.
const coordProgressUpdateInterval = 5 * time.Second

// Coordinator is a WebSockets server that allows workers to connect to it to
// obtain configuration information. It does nothing but coordinate load
// testing amongst the workers.
type Coordinator struct {
	cfg      *Config
	coordCfg *CoordinatorConfig
	logger   logging.Logger

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
	stateMetric            prometheus.Gauge // A code-based status metric for representing the coordinator's current state.
	totalTxsMetric         prometheus.Gauge // The total number of transactions sent by all workers.
	totalBytesMetric       prometheus.Gauge // The total cumulative bytes in transactions sent by all workers.
	txRateMetric           prometheus.Gauge // The transaction throughput rate (tx/sec) as measured by the coordinator since the last metrics update.
	txDataRateMetric       prometheus.Gauge // The total transaction throughput rate in bytes/sec as measured by the coordinator.
	overallTxRateMetric    prometheus.Gauge // The overall transaction throughput rate (tx/sec) as measured by the coordinator since the beginning of the load test.
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

func NewCoordinator(cfg *Config, coordCfg *CoordinatorConfig) *Coordinator {
	logger := logging.NewLogrusLogger("coordinator")
	coord := &Coordinator{
		cfg:                 cfg,
		coordCfg:            coordCfg,
		logger:              logger,
		svrStopped:          make(chan struct{}, 1),
		workers:             make(map[string]*remoteWorker),
		workerRegister:      make(chan remoteWorkerRegisterRequest, coordCfg.ExpectWorkers),
		workerUnregister:    make(chan remoteWorkerUnregisterRequest, coordCfg.ExpectWorkers),
		workerUpdate:        make(chan workerMsg, coordCfg.ExpectWorkers),
		stop:                make(chan struct{}, 1),
		totalTxsPerWorker:   make(map[string]int),
		totalBytesPerWorker: make(map[string]int64),
		stateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_coordinator_state",
			Help: "The current state of the tm-load-test coordinator",
		}),
		totalTxsMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_coordinator_total_txs",
			Help: "The total cumulative number of transactions sent by all workers",
		}),
		totalBytesMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_coordinator_total_bytes",
			Help: "The total cumulative number of bytes of transactions sent by all workers",
		}),
		txRateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_coordinator_tx_rate",
			Help: "The current transaction throughput rate (in txs/sec) as seen by the tm-load-test coordinator, summed across all workers",
		}),
		txDataRateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_coordinator_tx_data_rate",
			Help: "The current transaction throughput rate (in bytes/sec) as seen by the tm-load-test coordinator, summed across all workers",
		}),
		overallTxRateMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_coordinator_overall_tx_rate",
			Help: "The overall transaction throughput rate as seen by the tm-load-test coordinator since the beginning of the load test",
		}),
		workersCompletedMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_coordinator_workers_completed",
			Help: "The total number of workers that have completed their testing so far",
		}),
		testUnderwayMetric: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "tmloadtest_coordinator_test_underway",
			Help: "The ID of the load test currently underway (-1 if none)",
		}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", coord.newWebSocketHandler())
	mux.Handle("/metrics", promhttp.Handler())
	svr := &http.Server{
		Addr:    coordCfg.BindAddr,
		Handler: mux,
	}
	coord.svr = svr
	coord.stateMetric.Set(coordStarting)
	coord.testUnderwayMetric.Set(-1)
	return coord
}

// Run will execute the coordinator's operations in a blocking manner,
// returning any error that causes one of the workers or the coordinator to
// fail.
func (c *Coordinator) Run() error {
	// if we care about how many peers are connected in the network, wait
	// for a minimum number of them to connect before even listening for
	// incoming worker connections
	if c.cfg.ExpectPeers > 0 {
		if err := c.waitForPeers(); err != nil {
			c.stateMetric.Set(coordFailed)
			return err
		}
	}

	defer c.gracefulShutdown()

	// we want to know if the user hits Ctrl+Break
	cancelTrap := trapInterrupts(func() {
		c.setCancelled(true)
		close(c.stop)
		c.stateMetric.Set(coordFailed)
	}, c.logger)
	defer func() {
		close(cancelTrap)
	}()

	// we run the WebSockets server in the background
	go c.runServer()

	if err := c.waitForWorkers(); err != nil {
		c.failAllRemoteWorkers(err.Error())
		c.stateMetric.Set(coordFailed)
		return err
	}

	if err := c.receiveTestingUpdates(); err != nil {
		c.failAllRemoteWorkers(err.Error())
		c.stateMetric.Set(coordFailed)
		return err
	}

	c.stateMetric.Set(coordCompleted)
	return nil
}

func (c *Coordinator) waitForWorkers() error {
	c.logger.Info("Waiting for all workers to connect and register")
	c.stateMetric.Set(coordWaitingForWorkers)

	timeoutTicker := time.NewTicker(time.Duration(c.coordCfg.WorkerConnectTimeout) * time.Second)
	defer timeoutTicker.Stop()

	for {
		select {
		case req := <-c.workerRegister:
			req.resp <- c.registerRemoteWorker(req.rw)
			if len(c.workers) >= c.coordCfg.ExpectWorkers {
				return c.startLoadTest()
			}

		case req := <-c.workerUnregister:
			// we can do this safely during this waiting period without
			// jeopardizing the load testing
			c.unregisterRemoteWorker(req.id)

		case <-timeoutTicker.C:
			return fmt.Errorf("timed out waiting for all workers to connect")

		case <-c.stop:
			return fmt.Errorf("wait routine cancelled")

		case <-c.svrStopped:
			return fmt.Errorf("web server stopped unexpectedly")
		}
	}
}

// Starts with the configuration file's endpoints list, polling those endpoints
// for unique peers that have connected. Waits until we have the minimum number
// of endpoints, and on success returns a list of peer addresses. On failure,
// returns a relevant error.
func (c *Coordinator) waitForPeers() error {
	c.stateMetric.Set(coordWaitingForPeers)
	peers, err := waitForNetworkPeers(
		c.cfg.Endpoints,
		c.cfg.EndpointSelectMethod,
		c.cfg.ExpectPeers,
		c.cfg.MinConnectivity,
		c.cfg.MaxEndpoints,
		time.Duration(c.cfg.PeerConnectTimeout)*time.Second,
		c.logger,
	)
	if err != nil {
		c.logger.Error("Failed while waiting for peers to connect", "err", err)
		return err
	}
	c.cfg.Endpoints = peers
	return nil
}

func (c *Coordinator) receiveTestingUpdates() error {
	c.logger.Info("Watching for worker updates")
	c.stateMetric.Set(coordTesting)

	// we set the current test underway ID to our configured load test ID for
	// the duration of the test
	c.testUnderwayMetric.Set(float64(c.coordCfg.LoadTestID))
	// and we set it to -1 the moment all workers are done
	defer c.testUnderwayMetric.Set(-1)

	completed := 0

	progressTicker := time.NewTicker(coordProgressUpdateInterval)
	defer progressTicker.Stop()

	c.startTime = time.Now()
	c.lastProgressUpdate = c.startTime

	for {
		select {
		case msg := <-c.workerUpdate:
			c.logger.Debug("Got update from worker", "msg", msg)
			if _, exists := c.workers[msg.ID]; !exists {
				c.logger.Error("Got message from unregistered worker - ignoring", "id", msg.ID)
				continue
			}
			// keep track of how many transactions this worker has reported
			if msg.TxCount > 0 {
				c.totalTxsPerWorker[msg.ID] = msg.TxCount
			}
			// keep track of how many bytes this worker reported
			if msg.TotalTxBytes > 0 {
				c.totalBytesPerWorker[msg.ID] = msg.TotalTxBytes
			}

			switch msg.State {
			case workerTesting:
				c.logger.Debug("Update from remote worker", "id", msg.ID, "txCount", msg.TxCount)

			case workerCompleted:
				c.logger.Debug("Worker completed its testing", "id", msg.ID)
				completed++
				if completed >= c.coordCfg.ExpectWorkers {
					c.logger.Info("All workers completed their load testing")
					c.logTestingProgress(completed)
					return nil
				}

			case workerFailed:
				return fmt.Errorf(msg.Error)

			default:
				return fmt.Errorf("unexpected state from remote worker: %s", msg.State)
			}

		case req := <-c.workerUnregister:
			c.unregisterRemoteWorker(req.id)
			if req.err != nil {
				return fmt.Errorf("remote worker failed: %s", req.err.Error())
			}

		case <-progressTicker.C:
			c.logTestingProgress(completed)

		case <-c.stop:
			c.logger.Debug("Load testing cancel signal received")
			return fmt.Errorf("load testing cancelled")

		case <-c.svrStopped:
			return fmt.Errorf("web server stopped unexpectedly")
		}
	}
}

func (c *Coordinator) RegisterRemoteWorker(rw *remoteWorker) error {
	c.logger.Debug("Attempting to register remote worker")
	resp := make(chan error, 1)
	c.workerRegister <- remoteWorkerRegisterRequest{
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

func (c *Coordinator) registerRemoteWorker(rw *remoteWorker) error {
	c.logger.Debug("Attempting to register remote worker", "id", rw.ID())
	if len(c.workers) >= c.coordCfg.ExpectWorkers {
		return fmt.Errorf("too many workers")
	}
	id := rw.ID()
	if _, exists := c.workers[id]; exists {
		return fmt.Errorf("worker with ID %s already exists", id)
	}
	c.workers[id] = rw
	c.totalTxsPerWorker[id] = 0
	c.totalBytesPerWorker[id] = 0
	c.logger.Info("Added remote worker", "id", id)
	return nil
}

func (c *Coordinator) UnregisterRemoteWorker(id string, err error) {
	c.workerUnregister <- remoteWorkerUnregisterRequest{id: id, err: err}
}

func (c *Coordinator) unregisterRemoteWorker(id string) {
	delete(c.workers, id)
	c.logger.Info("Unregistered worker", "id", id)
}

func (c *Coordinator) ReceiveWorkerUpdate(msg workerMsg) {
	c.workerUpdate <- msg
}

func (c *Coordinator) logTestingProgress(completed int) {
	totalTxs := 0
	for _, txCount := range c.totalTxsPerWorker {
		totalTxs += txCount
	}
	totalBytes := int64(0)
	for _, txBytes := range c.totalBytesPerWorker {
		totalBytes += txBytes
	}
	overallElapsed := time.Since(c.startTime).Seconds()
	elapsed := time.Since(c.lastProgressUpdate).Seconds()

	overallAvgRate := float64(0)
	avgRate := float64(0)
	avgDataRate := float64(0)

	if overallElapsed > 0 {
		overallAvgRate = float64(totalTxs) / overallElapsed
	}
	if elapsed > 0 {
		avgRate = float64(totalTxs-c.totalTxs) / elapsed
		avgDataRate = float64(totalBytes-c.totalBytes) / elapsed
	}

	c.logger.Info(
		"Progress",
		"totalTxs", totalTxs,
		"overallAvgRate", fmt.Sprintf("%.2f txs/sec", overallAvgRate),
		"avgRate", fmt.Sprintf("%.2f txs/sec", avgRate),
		"totalBytes", totalBytes,
	)

	c.lastProgressUpdate = time.Now()
	c.totalTxs = totalTxs
	c.totalBytes = totalBytes
	c.totalTxsMetric.Set(float64(totalTxs))
	c.totalBytesMetric.Set(float64(totalBytes))
	c.txRateMetric.Set(avgRate)
	c.txDataRateMetric.Set(avgDataRate)
	c.overallTxRateMetric.Set(overallAvgRate)
	c.workersCompletedMetric.Set(float64(completed))

	// if we're done and we need to write aggregate statistics
	if completed >= c.coordCfg.ExpectWorkers && len(c.cfg.StatsOutputFile) > 0 {
		stats := AggregateStats{
			TotalTxs:         totalTxs,
			TotalTimeSeconds: overallElapsed,
			TotalBytes:       totalBytes,
		}
		if err := writeAggregateStats(c.cfg.StatsOutputFile, stats); err != nil {
			c.logger.Error("Failed to write aggregate statistics", "err", err)
		}
	}
}

func (c *Coordinator) startLoadTest() error {
	c.logger.Info("All workers connected - starting load test", "count", len(c.workers))
	for id, rw := range c.workers {
		if err := rw.StartLoadTest(); err != nil {
			c.logger.Info("Failed to start load test for worker", "id", id, "err", err)
			return err
		}
	}
	return nil
}

func (c *Coordinator) failAllRemoteWorkers(reason string) {
	c.logger.Debug("Failing all remote workers", "reason", reason)
	for _, rw := range c.workers {
		_ = rw.Fail(reason)
	}
	c.logger.Debug("Failed all remote workers")
}

func (c *Coordinator) newWebSocketHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			c.logger.Error("Error while attempting to upgrade incoming WebSockets connection", "err", err)
			return
		}
		defer conn.Close()

		c.logger.Debug("Received incoming WebSockets connection", "r", r)
		newRemoteWorker(conn, c).Run()
	}
}

func (c *Coordinator) runServer() {
	defer close(c.svrStopped)

	c.logger.Info("Starting WebSockets server")

	if err := c.svr.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		c.logger.Error("Server shut down", "err", err)
		return
	}
	c.logger.Info("Server shut down")
}

// Graceful shutdown for the web server.
func (c *Coordinator) shutdownServer() {
	// we only care about the shutdown wait period if we haven't been killed
	if !c.wasCancelled() && c.coordCfg.ShutdownWait > 0 {
		c.logger.Info("Entering post-shutdown wait period", "wait", fmt.Sprintf("%ds", c.coordCfg.ShutdownWait))
		cancelSleep := make(chan struct{})
		cancelTrap := trapInterrupts(func() { close(cancelSleep) }, c.logger)
		select {
		case <-cancelSleep:
			c.logger.Info("Cancelling shutdown wait")
		case <-time.After(time.Duration(c.coordCfg.ShutdownWait) * time.Second):
		}
		close(cancelTrap)
	}
	c.logger.Info("Shutting down web server")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.svr.Shutdown(ctx); err != nil {
		c.logger.Error("Failed to gracefully shut down web server", "err", err)
	} else {
		c.logger.Info("Shut down web server")
	}
}

func (c *Coordinator) expectedWorkers() int {
	return c.coordCfg.ExpectWorkers
}

func (c *Coordinator) config() Config {
	return *c.cfg
}

func (c *Coordinator) stopRemoteWorkers() {
	c.logger.Debug("Stopping all remote workers")
	for _, rw := range c.workers {
		rw.Stop()
	}
}

func (c *Coordinator) gracefulShutdown() {
	// stop all remote worker event loops
	c.stopRemoteWorkers()
	// gracefully shut down the WebSockets server
	c.shutdownServer()
	select {
	case <-c.svrStopped:
	case <-time.After(coordShutdownTimeout):
		c.logger.Error("Failed to shut down within the required time period")
	}
}

func (c *Coordinator) setCancelled(cancelled bool) {
	c.mtx.Lock()
	c.cancelled = true
	c.mtx.Unlock()
}

func (c *Coordinator) wasCancelled() bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.cancelled
}
