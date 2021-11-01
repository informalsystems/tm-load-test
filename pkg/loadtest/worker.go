package loadtest

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"github.com/informalsystems/tm-load-test/internal/logging"
	uuid "github.com/satori/go.uuid"
)

const (
	workerUpdateInterval       = 3 * time.Second
	workerConnectRetryInterval = 1 * time.Second
	workerStartPollTimeout     = 60 * time.Second
)

// Worker is a WebSockets client that interacts with the Coordinator node to (1) fetch
// its configuration, (2) execute a load test, and (3) report back to the coordinator
// node regularly on its progress.
type Worker struct {
	workerCfg *WorkerConfig
	sock      *simpleSocket
	logger    logging.Logger

	idMtx sync.RWMutex
	id    string

	cfgMtx sync.RWMutex
	cfg    Config

	interruptsMtx sync.RWMutex
	interrupts    map[string]func()

	stop     chan struct{}
	stopped  chan struct{}
	tgCancel chan error // Send errors here to cancel the TransactorGroup's operations.
}

func NewWorker(cfg *WorkerConfig) (*Worker, error) {
	workerID := cfg.ID
	if len(workerID) == 0 {
		workerID = makeWorkerID()
	}
	if !isValidWorkerID(workerID) {
		return nil, fmt.Errorf("invalid worker ID \"%s\": worker IDs can only contain lowercase alphanumeric characters", workerID)
	}
	return &Worker{
		id:         workerID,
		workerCfg:  cfg,
		logger:     logging.NewLogrusLogger(fmt.Sprintf("worker[%s]", workerID)),
		interrupts: make(map[string]func()),
		stop:       make(chan struct{}, 1),
		stopped:    make(chan struct{}, 1),
		tgCancel:   make(chan error, 1),
	}, nil
}

// Run executes the primary event loop for this worker.
func (w *Worker) Run() error {
	defer close(w.stopped)

	cancelTrap := trapInterrupts(func() { w.cancel() }, w.logger)
	defer close(cancelTrap)

	if err := w.connectToCoordinator(); err != nil {
		w.logger.Error("Failed to connect to coordinator", "err", err)
		return err
	}
	defer w.close()

	// we run our socket operations in a separate goroutine
	go w.sock.Run()

	if err := w.register(); err != nil {
		w.logger.Error("Failed to register with coordinator", "err", err)
		return err
	}

	if err := w.waitForStart(); err != nil {
		w.logger.Error("Failed while waiting for load test to start", "err", err)
		w.fail(err.Error())
		return err
	}

	if err := w.executeLoadTest(); err != nil {
		w.logger.Error("Failed during load testing", "err", err)
		w.fail(err.Error())
		return err
	}

	return nil
}

func (w *Worker) ID() string {
	w.idMtx.RLock()
	defer w.idMtx.RUnlock()
	return w.id
}

func (w *Worker) setCfg(cfg Config) {
	w.cfgMtx.Lock()
	w.cfg = cfg
	w.cfgMtx.Unlock()
}

func (w *Worker) Config() Config {
	w.cfgMtx.RLock()
	defer w.cfgMtx.RUnlock()
	return w.cfg
}

func (w *Worker) triggerInterrupts() {
	w.interruptsMtx.RLock()
	defer w.interruptsMtx.RUnlock()
	for _, interrupt := range w.interrupts {
		interrupt()
	}
}

func (w *Worker) setInterrupt(id string, interrupt func()) {
	w.interruptsMtx.Lock()
	w.interrupts[id] = interrupt
	w.interruptsMtx.Unlock()
}

func (w *Worker) removeInterrupt(id string) {
	w.interruptsMtx.Lock()
	delete(w.interrupts, id)
	w.interruptsMtx.Unlock()
}

func (w *Worker) connectToCoordinator() error {
	timeoutTicker := time.NewTicker(time.Duration(w.workerCfg.CoordConnectTimeout) * time.Second)
	defer timeoutTicker.Stop()

	w.logger.Info("Waiting for successful connection to remote coordinator", "addr", w.workerCfg.CoordAddr)

	for {
		conn, _, err := websocket.DefaultDialer.Dial(w.workerCfg.CoordAddr, nil)
		if err == nil {
			w.logger.Info("Successfully connected to remote coordinator")
			w.sock = newSimpleSocket(
				conn,
				ssInboundBufSize(10),
				ssOutboundBufSize(10),
				ssFlushOnStop(true),
				ssSendCloseMessage(true),
				ssWaitForRemoteClose(true),
				ssRemoteCloseWaitTimeout(60*time.Second),
				ssParentCtx(fmt.Sprintf("worker[%s]", w.ID())),
			)
			return nil
		}
		w.logger.Debug(
			fmt.Sprintf(
				"Failed to connect to remote coordinator - retrying in %s",
				workerConnectRetryInterval.String(),
			),
			"err", err,
		)

		select {
		case <-timeoutTicker.C:
			return fmt.Errorf("failed to reach coordinator within connect time limit")

		case <-w.stop:
			return fmt.Errorf("worker operations cancelled")

		case <-time.After(workerConnectRetryInterval):
			w.logger.Debug("Retrying to connect to coordinator")
		}
	}
}

func (w *Worker) register() error {
	w.logger.Info("Registering with coordinator")
	if err := w.sock.WriteWorkerMsg(workerMsg{ID: w.ID()}); err != nil {
		return err
	}
	// now wait for a response from the coordinator
	resp, err := w.sock.ReadWorkerMsg()
	if err != nil {
		return err
	}
	if resp.State != workerAccepted {
		return fmt.Errorf("coordinator did not accept worker with state: %s", resp.State)
	}
	if resp.Config == nil {
		// tell the coordinator there's a problem
		w.fail("missing configuration from coordinator")
		return fmt.Errorf("missing configuration from coordinator")
	}
	// we need to check the coordinator on its configuration
	if err := resp.Config.Validate(); err != nil {
		_ = w.sock.WriteWorkerMsg(workerMsg{ID: w.ID(), State: workerFailed, Error: err.Error()})
		return err
	}

	// quick check if we've been cancelled
	select {
	case <-w.stop:
		return fmt.Errorf("worker operations cancelled")
	default:
	}

	w.setCfg(*resp.Config)
	w.logger.Info("Successfully registered with coordinator")
	w.logger.Debug("Got load testing configuration from coordinator", "cfg", w.Config().ToJSON())
	return nil
}

func (w *Worker) waitForStart() error {
	w.logger.Info("Waiting for signal from coordinator to start load test")

	var msg workerMsg
	var err error

	for {
		w.logger.Debug("Polling coordinator for ready message")
		// try to read a message from the coordinator
		msg, err = w.sock.ReadWorkerMsg(workerStartPollTimeout)
		if err == nil {
			break
		}

		select {
		case <-w.stop:
			return fmt.Errorf("worker operations cancelled")

		default:
		}
	}

	if msg.State != workerTesting {
		return fmt.Errorf("unexpected state change from coordinator: %s", msg.State)
	}

	w.logger.Info("Coordinator initiated load test")
	return nil
}

func (w *Worker) executeLoadTest() error {
	w.logger.Info("Connecting to remote endpoints")
	tg := NewTransactorGroup()
	cfg := w.Config()
	if err := tg.AddAll(&cfg); err != nil {
		return err
	}
	tg.SetProgressCallback(workerUpdateInterval, w.reportProgress)

	w.logger.Info("Initiating load test")
	tg.Start()

	w.setInterrupt("ExecuteStandalone", func() { tg.Cancel() })
	defer w.removeInterrupt("ExecuteStandalone")

	if err := tg.Wait(); err != nil {
		w.logger.Error("Failed to execute load test", "err", err)
		return err
	}

	// send the completion notification to the coordinator
	if err := w.reportFinalResults(tg.totalTxs(), tg.totalBytes()); err != nil {
		w.logger.Error("Failed to report final results for load test", "err", err)
		return err
	}

	w.logger.Info("Load test complete!")
	return nil
}

func (w *Worker) reportProgress(tg *TransactorGroup, totalTxs int, totalTxBytes int64) {
	w.logger.Debug("Reporting progress back to coordinator", "totalTxs", totalTxs)
	if err := w.sock.WriteWorkerMsg(workerMsg{
		ID:           w.ID(),
		State:        workerTesting,
		TxCount:      totalTxs,
		TotalTxBytes: totalTxBytes,
	}); err != nil {
		w.logger.Error("Failed to report progress to coordinator", "err", err)
		tg.Cancel()
	}
}

func (w *Worker) reportFinalResults(totalTxs int, totalTxBytes int64) error {
	w.logger.Debug("Reporting final results back to coordinator", "totalTxs", totalTxs)
	return w.sock.WriteWorkerMsg(workerMsg{
		ID:           w.ID(),
		State:        workerCompleted,
		TxCount:      totalTxs,
		TotalTxBytes: totalTxBytes,
	})
}

func (w *Worker) fail(reason string) {
	_ = w.sock.WriteWorkerMsg(workerMsg{ID: w.ID(), State: workerFailed, Error: reason})
}

func (w *Worker) cancel() {
	w.logger.Error("Worker operations cancelled")
	defer close(w.stop)
	w.triggerInterrupts()
}

func (w *Worker) close() {
	w.sock.Stop()
	w.logger.Info("Closed connection to remote coordinator")
}

func isValidWorkerID(id string) bool {
	for _, r := range id {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func makeWorkerID() string {
	return strings.ReplaceAll(uuid.NewV4().String(), "-", "")
}
