package loadtest

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"github.com/interchainio/tm-load-test/internal/logging"
	uuid "github.com/satori/go.uuid"
)

const (
	slaveUpdateInterval       = 3 * time.Second
	slaveConnectRetryInterval = 1 * time.Second
	slaveStartPollTimeout     = 60 * time.Second
)

// Slave is a WebSockets client that interacts with the Master node to (1) fetch
// its configuration, (2) execute a load test, and (3) report back to the master
// node regularly on its progress.
type Slave struct {
	slaveCfg *SlaveConfig
	sock     *simpleSocket
	logger   logging.Logger

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

func NewSlave(cfg *SlaveConfig) (*Slave, error) {
	slaveID := cfg.ID
	if len(slaveID) == 0 {
		slaveID = makeSlaveID()
	}
	if !isValidSlaveID(slaveID) {
		return nil, fmt.Errorf("invalid slave ID \"%s\": slave IDs can only contain lowercase alphanumeric characters", slaveID)
	}
	return &Slave{
		id:         slaveID,
		slaveCfg:   cfg,
		logger:     logging.NewLogrusLogger("slave"),
		interrupts: make(map[string]func()),
		stop:       make(chan struct{}, 1),
		stopped:    make(chan struct{}, 1),
		tgCancel:   make(chan error, 1),
	}, nil
}

// Run executes the primary event loop for this slave.
func (s *Slave) Run() error {
	defer close(s.stopped)

	cancelTrap := trapInterrupts(func() { s.cancel() }, s.logger)
	defer close(cancelTrap)

	if err := s.connectToMaster(); err != nil {
		s.logger.Error("Failed to connect to master", "err", err)
		return err
	}
	defer s.close()

	// we run our socket operations in a separate goroutine
	go s.sock.Run()

	if err := s.register(); err != nil {
		s.logger.Error("Failed to register with master", "err", err)
		return err
	}

	if err := s.waitForStart(); err != nil {
		s.logger.Error("Failed while waiting for load test to start", "err", err)
		s.fail(err.Error())
		return err
	}

	if err := s.executeLoadTest(); err != nil {
		s.logger.Error("Failed during load testing", "err", err)
		s.fail(err.Error())
		return err
	}

	return nil
}

func (s *Slave) ID() string {
	s.idMtx.RLock()
	defer s.idMtx.RUnlock()
	return s.id
}

func (s *Slave) setCfg(cfg Config) {
	s.cfgMtx.Lock()
	s.cfg = cfg
	s.cfgMtx.Unlock()
}

func (s *Slave) Config() Config {
	s.cfgMtx.RLock()
	defer s.cfgMtx.RUnlock()
	return s.cfg
}

func (s *Slave) triggerInterrupts() {
	s.interruptsMtx.RLock()
	defer s.interruptsMtx.RUnlock()
	for _, interrupt := range s.interrupts {
		interrupt()
	}
}

func (s *Slave) setInterrupt(id string, interrupt func()) {
	s.interruptsMtx.Lock()
	s.interrupts[id] = interrupt
	s.interruptsMtx.Unlock()
}

func (s *Slave) removeInterrupt(id string) {
	s.interruptsMtx.Lock()
	delete(s.interrupts, id)
	s.interruptsMtx.Unlock()
}

func (s *Slave) connectToMaster() error {
	timeoutTicker := time.NewTicker(time.Duration(s.slaveCfg.MasterConnectTimeout) * time.Second)
	defer timeoutTicker.Stop()

	s.logger.Info("Waiting for successful connection to remote master", "addr", s.slaveCfg.MasterAddr)

	for {
		conn, _, err := websocket.DefaultDialer.Dial(s.slaveCfg.MasterAddr, nil)
		if err == nil {
			s.logger.Info("Successfully connected to remote master")
			s.sock = newSimpleSocket(
				conn,
				ssInboundBufSize(10),
				ssOutboundBufSize(10),
				ssFlushOnStop(true),
				ssSendCloseMessage(true),
				ssWaitForRemoteClose(true),
				ssRemoteCloseWaitTimeout(60*time.Second),
				ssParentCtx("slave"),
			)
			return nil
		}
		s.logger.Debug(
			fmt.Sprintf(
				"Failed to connect to remote master - retrying in %s",
				slaveConnectRetryInterval.String(),
			),
			"err", err,
		)

		select {
		case <-timeoutTicker.C:
			return fmt.Errorf("failed to reach master within connect time limit")

		case <-s.stop:
			return fmt.Errorf("slave operations cancelled")

		case <-time.After(slaveConnectRetryInterval):
			s.logger.Debug("Retrying to connect to master")
		}
	}
}

func (s *Slave) register() error {
	s.logger.Info("Registering with master")
	if err := s.sock.WriteSlaveMsg(slaveMsg{ID: s.ID()}); err != nil {
		return err
	}
	// now wait for a response from the master
	resp, err := s.sock.ReadSlaveMsg()
	if err != nil {
		return err
	}
	if resp.State != slaveAccepted {
		return fmt.Errorf("master did not accept slave with state: %s", resp.State)
	}
	if resp.Config == nil {
		// tell the master there's a problem
		s.fail("missing configuration from master")
		return fmt.Errorf("missing configuration from master")
	}
	// we need to check the master on its configuration
	if err := resp.Config.Validate(); err != nil {
		_ = s.sock.WriteSlaveMsg(slaveMsg{ID: s.ID(), State: slaveFailed, Error: err.Error()})
		return err
	}

	// quick check if we've been cancelled
	select {
	case <-s.stop:
		return fmt.Errorf("slave operations cancelled")
	default:
	}

	s.setCfg(*resp.Config)
	s.logger.Info("Successfully registered with master")
	s.logger.Debug("Got load testing configuration from master", "cfg", s.Config().ToJSON())
	return nil
}

func (s *Slave) waitForStart() error {
	s.logger.Info("Waiting for signal from master to start load test")

	var msg slaveMsg
	var err error

	for {
		s.logger.Debug("Polling master for ready message")
		// try to read a message from the master
		msg, err = s.sock.ReadSlaveMsg(slaveStartPollTimeout)
		if err == nil {
			break
		}

		select {
		case <-s.stop:
			return fmt.Errorf("slave operations cancelled")

		default:
		}
	}

	if msg.State != slaveTesting {
		return fmt.Errorf("unexpected state change from master: %s", msg.State)
	}

	s.logger.Info("Master initiated load test")
	return nil
}

func (s *Slave) executeLoadTest() error {
	s.logger.Info("Connecting to remote endpoints")
	tg := NewTransactorGroup()
	cfg := s.Config()
	if err := tg.AddAll(&cfg); err != nil {
		return err
	}
	tg.SetProgressCallback(slaveUpdateInterval, s.reportProgress)

	s.logger.Info("Initiating load test")
	tg.Start()

	s.setInterrupt("executeLoadTest", func() { tg.Cancel() })
	defer s.removeInterrupt("executeLoadTest")

	if err := tg.Wait(); err != nil {
		s.logger.Error("Failed to execute load test", "err", err)
		return err
	}

	// send the completion notification to the master
	if err := s.reportFinalResults(tg.totalTxs()); err != nil {
		s.logger.Error("Failed to report final results for load test", "err", err)
		return err
	}

	s.logger.Info("Load test complete!")
	return nil
}

func (s *Slave) reportProgress(tg *TransactorGroup, totalTxs int) {
	s.logger.Debug("Reporting progress back to master", "totalTxs", totalTxs)
	if err := s.sock.WriteSlaveMsg(slaveMsg{ID: s.ID(), State: slaveTesting, TxCount: totalTxs}); err != nil {
		s.logger.Error("Failed to report progress to master", "err", err)
		tg.Cancel()
	}
}

func (s *Slave) reportFinalResults(totalTxs int) error {
	s.logger.Debug("Reporting final results back to master", "totalTxs", totalTxs)
	return s.sock.WriteSlaveMsg(slaveMsg{ID: s.ID(), State: slaveCompleted, TxCount: totalTxs})
}

func (s *Slave) fail(reason string) {
	_ = s.sock.WriteSlaveMsg(slaveMsg{ID: s.ID(), State: slaveFailed, Error: reason})
}

func (s *Slave) cancel() {
	s.logger.Error("Slave operations cancelled")
	defer close(s.stop)
	s.triggerInterrupts()
}

func (s *Slave) close() {
	s.sock.Stop()
	s.logger.Info("Closed connection to remote master")
}

func isValidSlaveID(id string) bool {
	for _, r := range id {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func makeSlaveID() string {
	return strings.ReplaceAll(uuid.NewV4().String(), "-", "")
}
