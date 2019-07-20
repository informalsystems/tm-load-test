package loadtest

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/interchainio/tm-load-test/internal/logging"
)

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

	slaveRegistration chan remoteSlaveRegistrationRequest // Send a request here to register a remote slave.
	slaveUpdate       chan slaveMsg
	stop              chan struct{}

	// Rudimentary statistics
	startTime          time.Time
	lastProgressUpdate time.Time
	totalTxs           int
}

type remoteSlaveRegistrationRequest struct {
	rs   *remoteSlave
	resp chan error
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func NewMaster(cfg *Config, masterCfg *MasterConfig) *Master {
	logger := logging.NewLogrusLogger("master")
	master := &Master{
		cfg:               cfg,
		masterCfg:         masterCfg,
		logger:            logger,
		svrStopped:        make(chan struct{}, 1),
		slaves:            make(map[string]*remoteSlave),
		slaveRegistration: make(chan remoteSlaveRegistrationRequest, masterCfg.ExpectSlaves),
		stop:              make(chan struct{}, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/websocket", master.newWebSocketHandler())
	svr := &http.Server{
		Addr:    masterCfg.BindAddr,
		Handler: mux,
	}
	master.svr = svr
	return master
}

// Run will execute the master's operations in a blocking manner, returning
// any error that causes one of the slaves or the master to fail.
func (m *Master) Run() error {
	defer func() {
		// gracefully shut down the WebSockets server
		m.shutdownServer()
		<-m.svrStopped
	}()

	sigc := make(chan os.Signal, 1)
	cancelTrap := make(chan struct{})
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigc:
			m.logger.Info("Caught kill signal")
			close(m.stop)

		case <-cancelTrap:
			return
		}
	}()
	defer close(cancelTrap)

	// we run the WebSockets server in the background
	go m.runServer()

	if err := m.waitForSlaves(); err != nil {
		m.failAllRemoteSlaves(err.Error())
		return err
	}

	if err := m.receiveTestingUpdates(); err != nil {
		m.failAllRemoteSlaves(err.Error())
		return err
	}

	return nil
}

func (m *Master) waitForSlaves() error {
	m.logger.Info("Waiting for all slaves to connect and register")
	timeoutTicker := time.NewTicker(time.Duration(m.masterCfg.SlaveConnectTimeout) * time.Second)
	defer timeoutTicker.Stop()

	for {
		select {
		case req := <-m.slaveRegistration:
			req.resp <- m.registerRemoteSlave(req.rs)
			if len(m.slaves) >= m.masterCfg.ExpectSlaves {
				return m.startLoadTest()
			}

		case <-timeoutTicker.C:
			return fmt.Errorf("timed out waiting for all slaves to connect")

		case <-m.stop:
			return fmt.Errorf("wait routine cancelled")
		}
	}
}

func (m *Master) receiveTestingUpdates() error {
	m.logger.Info("Watching for slave updates")
	completed := 0

	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

	m.startTime = time.Now()
	m.lastProgressUpdate = m.startTime

	for {
		select {
		case msg := <-m.slaveUpdate:
			if _, exists := m.slaves[msg.ID]; !exists {
				m.logger.Error("Got message from unregistered slave - ignoring", "id", msg.ID)
				continue
			}

			switch msg.State {
			case slaveTesting:
				m.logger.Debug("Update from remote slave", "id", msg.ID, "txCount", msg.TxCount)

			case slaveCompleted:
				completed++
				if completed >= m.masterCfg.ExpectSlaves {
					m.logger.Debug("Slave completed its testing", "id", msg.ID)
					return nil
				}

			case slaveFailed:
				return fmt.Errorf(msg.Error)

			default:
				return fmt.Errorf("unexpected state from remote slave: %s", msg.State)
			}

		case <-progressTicker.C:
			m.logTestingProgress()

		case <-m.stop:
			return fmt.Errorf("load testing cancelled")
		}
	}
}

func (m *Master) RegisterRemoteSlave(rs *remoteSlave) error {
	resp := make(chan error, 1)
	m.slaveRegistration <- remoteSlaveRegistrationRequest{
		rs:   rs,
		resp: resp,
	}
	return <-resp
}

func (m *Master) registerRemoteSlave(rs *remoteSlave) error {
	if len(m.slaves) >= m.masterCfg.ExpectSlaves {
		return fmt.Errorf("too many slaves")
	}
	id := rs.ID()
	if _, exists := m.slaves[id]; exists {
		return fmt.Errorf("slave with ID %s already exists", id)
	}
	m.slaves[id] = rs
	m.logger.Info("Added remote slave", "id", id)
	return nil
}

func (m *Master) ReceiveSlaveUpdate(msg slaveMsg) {
	m.slaveUpdate <- msg
}

func (m *Master) logTestingProgress() {
	totalTxs := 0
	for _, rs := range m.slaves {
		totalTxs += rs.TxCount()
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

	m.totalTxs = totalTxs
	m.lastProgressUpdate = time.Now()
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
	for _, rs := range m.slaves {
		_ = rs.Fail(reason)
	}
}

func (m *Master) newWebSocketHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			m.logger.Error("Error while attempting to upgrade incoming WebSockets connection", "err", err)
			return
		}
		defer conn.Close()

		newRemoteSlave(conn, m).Run()
	}
}

func (m *Master) runServer() {
	defer close(m.svrStopped)

	if err := m.svr.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		m.logger.Error("Server shut down", "err", err)
		return
	}
	m.logger.Info("Server shut down")
}

// Graceful shutdown for the web server.
func (m *Master) shutdownServer() {
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
