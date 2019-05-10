package loadtest

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/tendermint/tendermint/rpc/client"
	"golang.org/x/crypto/bcrypt"
)

// The maximum number of failures to tolerate when attempting to query for peers
// from a Tendermint seed node before failing the master.
const defaultSeedPollMaxFailures = 5

// Master is an entity that coordinates the load testing across multiple slave
// nodes, tracking progress and failures.
type Master struct {
	cfg    *Config
	logger logging.Logger

	svr *baseServer

	mtx                  sync.Mutex
	summary              *Summary
	startTime            time.Time
	slaves               map[string]*remoteSlave
	flagKill             bool
	shutdownErr          error
	expectedInteractions int64
	targets              map[string]TestNetworkTargetConfig // For autodetection

	// For tracking HTTP long polling requests that're waiting for the go-ahead
	// to start testing.
	readySubscribers map[int]chan bool

	slavec chan slaveRequest
}

type slaveRequest struct {
	slave remoteSlave
	errc  chan error
}

type stillWaitingForTargets struct{}
type stillWaitingForSlaves struct{}

var _ error = (*stillWaitingForTargets)(nil)
var _ error = (*stillWaitingForSlaves)(nil)

// NewMaster instantiates a new master node for load testing.
func NewMaster(cfg *Config) (*Master, error) {
	logger := logging.NewLogrusLogger("master")
	m := &Master{
		cfg:                  cfg,
		logger:               logger,
		summary:              &Summary{},
		slaves:               make(map[string]*remoteSlave),
		expectedInteractions: int64(cfg.Master.ExpectSlaves) * int64(cfg.Clients.Spawn) * int64(cfg.Clients.MaxInteractions),
		readySubscribers:     make(map[int]chan bool),
		slavec:               make(chan slaveRequest, cfg.Master.ExpectSlaves),
		targets:              make(map[string]TestNetworkTargetConfig),
	}
	if cfg.Clients.MaxInteractions == -1 {
		m.expectedInteractions = -1
	}
	if m.expectedInteractions < -1 {
		return nil, NewError(ErrInvalidConfig, nil, "total expected interactions must be -1 or greater than or equal to 0")
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/slave", m.handleSlaveRequest)
	svr, err := newBaseServer(cfg.Master.Bind, mux, logger)
	if err != nil {
		return nil, err
	}
	m.svr = svr
	// set the random seed for generating readiness subscriber IDs
	rand.Seed(time.Now().UnixNano())
	m.logger.Debug("Created master", "expectedInteractions", m.expectedInteractions)
	return m, nil
}

// Run executes the entire load test synchronously, exiting only once the load
// test completes or fails.
func (m *Master) Run() (*Summary, error) {
	if err := m.svr.start(); err != nil {
		return nil, NewError(ErrMasterFailed, err)
	}
	defer m.shutdown()

	if m.cfg.TestNetwork.Autodetect.Enabled {
		if err := m.pollForTargets(); err != nil {
			return nil, m.fail(err)
		}
	}

	if err := m.waitForSlaves(); err != nil {
		return nil, m.fail(err)
	}

	if err := m.doLoadTest(); err != nil {
		m.fail(err) // nolint: errcheck
	}

	if m.getShutdownErr() == nil && m.cfg.Master.WaitAfterFinished > 0 {
		m.logger.Info(
			"Waiting predefined time period after completion of testing",
			"duration",
			m.cfg.Master.WaitAfterFinished.Duration().String(),
		)
		time.Sleep(m.cfg.Master.WaitAfterFinished.Duration())
	}

	return m.getSummary(), m.getShutdownErr()
}

func (m *Master) pollForTargets() error {
	m.logger.Info("Polling for targets", "seedNode", m.cfg.TestNetwork.Autodetect.SeedNode)
	killCheckTicker := time.NewTicker(100 * time.Millisecond)
	defer killCheckTicker.Stop()
	seedPollTicker := time.NewTicker(2 * time.Second)
	defer seedPollTicker.Stop()
	seedPollTimeout := time.NewTicker(m.cfg.TestNetwork.Autodetect.ExpectTargetsWithin.Duration())
	defer seedPollTimeout.Stop()

	c := client.NewHTTP(m.cfg.TestNetwork.Autodetect.SeedNode.String(), "/websocket")
	defer c.Stop() // nolint: errcheck
	errCount := 0

	for {
		select {
		// if a slave tries to connect while we're still trying to gather
		// targets
		case req := <-m.slavec:
			req.errc <- &stillWaitingForTargets{}

		case <-seedPollTicker.C:
			err := m.querySeedForTargets(c)
			if err != nil {
				errCount++
				m.logger.Debug("Failed to query seed node", "err", err, "errCount", errCount)
				if errCount > defaultSeedPollMaxFailures {
					return NewError(ErrMasterFailed, err, fmt.Sprintf("more than %d failures while attempting to query seed node for targets", defaultSeedPollMaxFailures))
				}
			}
			if m.allTargetsPresent() {
				m.logger.Info("Discovered all necessary targets for load testing", "count", m.cfg.TestNetwork.Autodetect.ExpectTargets)
				m.logTargets()
				return nil
			}

		case <-seedPollTimeout.C:
			return NewError(
				ErrMasterFailed,
				nil,
				fmt.Sprintf(
					"failed to find %d target nodes within %s",
					m.cfg.TestNetwork.Autodetect.ExpectTargets,
					m.cfg.TestNetwork.Autodetect.ExpectTargetsWithin.Duration().String(),
				),
			)

		case <-killCheckTicker.C:
			if m.killed() {
				return NewError(ErrMasterFailed, nil, "master killed")
			}
		}
	}
}

func (m *Master) querySeedForTargets(c *client.HTTP) error {
	info, err := c.NetInfo()
	if err != nil {
		return err
	}
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, peer := range info.Peers {
		peerURL, err := url.Parse(peer.NodeInfo.Other.RPCAddress)
		if err != nil {
			m.logger.Debug("Unrecognized peer RPC address format", "rpcAddress", peer.NodeInfo.Other.RPCAddress, "err", err)
			continue
		}
		m.targets[peer.RemoteIP] = TestNetworkTargetConfig{
			ID:  peer.NodeInfo.Moniker,
			URL: fmt.Sprintf("%s://%s:%s", peerURL.Scheme, peer.RemoteIP, peerURL.Port()),
		}
	}
	if m.cfg.TestNetwork.Autodetect.TargetSeedNode {
		m.targets[m.cfg.TestNetwork.Autodetect.SeedNode.String()] = TestNetworkTargetConfig{
			ID:  "seed_node",
			URL: m.cfg.TestNetwork.Autodetect.SeedNode.String(),
		}
	}
	m.logger.Debug("Got response from seed node", "targetCount", len(m.targets))
	return nil
}

func (m *Master) allTargetsPresent() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return len(m.targets) >= m.cfg.TestNetwork.Autodetect.ExpectTargets
}

func (m *Master) logTargets() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	i := 0
	for _, target := range m.targets {
		m.logger.Info(fmt.Sprintf("Target %d", i), "url", target.URL, "id", target.ID)
		i++
	}
}

func (m *Master) getTargets() []TestNetworkTargetConfig {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	targets := make([]TestNetworkTargetConfig, 0)
	for _, target := range m.targets {
		targets = append(targets, target)
	}
	return targets
}

func (m *Master) waitForSlaves() error {
	slavesConnected := make(chan struct{})
	killc := make(chan struct{})
	go func() {
		killCheckTicker := time.NewTicker(100 * time.Millisecond)
	connectLoop:
		for {
			select {
			case msg := <-m.slavec:
				msg.errc <- m.addSlave(&msg.slave)
				if m.ready() {
					m.logger.Info("All expected slaves have connected", "count", len(m.slaves))
					m.broadcastReady()
					m.trackStartTime()
					break connectLoop
				}

			case <-killCheckTicker.C:
				if m.killed() {
					break connectLoop
				}

			case <-killc:
				break connectLoop
			}
		}
		close(slavesConnected)
	}()
	// we have a deadline for the slaves to all connect
	select {
	case <-slavesConnected:
		return nil

	case <-time.After(m.cfg.Master.ExpectSlavesWithin.Duration()):
		// force the goroutine to terminate
		close(killc)
		<-slavesConnected
		return NewError(ErrTimedOutWaitingForSlaves, nil)
	}
}

func (m *Master) doLoadTest() error {
	killCheckTicker := time.NewTicker(100 * time.Millisecond)
	defer killCheckTicker.Stop()
	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()
	for {
		select {
		case msg := <-m.slavec:
			if err := m.handleMsgDuringTesting(msg); err != nil {
				return err
			}
			if m.done() {
				m.logger.Info("All slaves are finished with testing")
				m.computeFinalStats()
				return nil
			}

		case <-killCheckTicker.C:
			if m.killed() {
				return NewError(ErrMasterFailed, nil, "master killed")
			}

		case <-progressTicker.C:
			m.logProgress()
		}
	}
}

func (m *Master) logProgress() {
	if m.cfg.Clients.MaxInteractions == -1 {
		m.logTimeProgress()
	} else {
		m.logInteractionProgress()
	}
}

func (m *Master) logTimeProgress() {
	interactions := m.countInteractions()
	if interactions == 0 {
		return
	}
	totalSeconds := m.timeSinceStart().Seconds()
	expectedTestTime := m.cfg.Clients.MaxTestTime.Duration().Seconds()
	progress := float64(0)
	ips := float64(0)
	if totalSeconds > 0 {
		ips = float64(interactions) / totalSeconds
		progress = float64(100) * totalSeconds / expectedTestTime
	}
	timeLeft := time.Duration(int64((expectedTestTime-totalSeconds)*1000)) * time.Millisecond
	m.logger.Info(
		"Progress",
		"interactions", interactions,
		"progress", fmt.Sprintf("%.1f%%", progress),
		"interactionsPerSec", fmt.Sprintf("%.1f", ips),
		"timeLeft", timeLeft.String(),
	)
}

func (m *Master) logInteractionProgress() {
	interactions := m.countInteractions()
	if interactions == 0 {
		return
	}
	expectedInteractions := float64(m.getExpectedInteractions())
	progress := float64(100) * float64(interactions) / expectedInteractions
	totalSeconds := m.timeSinceStart().Seconds()
	ips := float64(0)
	if totalSeconds > 0 {
		ips = float64(interactions) / totalSeconds
	}
	// extrapolate to try to find how long left for the test
	expectedTotalSeconds := float64(0)
	if ips > 0 {
		expectedTotalSeconds = expectedInteractions / ips
	}
	timeLeft := time.Duration(int64(expectedTotalSeconds-totalSeconds)*1000) * time.Millisecond
	m.logger.Info(
		"Progress",
		"interactions", interactions,
		"progress", fmt.Sprintf("%.1f%%", progress),
		"interactionsPerSec", fmt.Sprintf("%.1f", ips),
		"estTimeLeft", timeLeft.String(),
	)
}

func (m *Master) timeSinceStart() time.Duration {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return time.Since(m.startTime)
}

func (m *Master) countInteractions() int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	var count int64
	for _, slave := range m.slaves {
		count += slave.Interactions
	}
	return count
}

func (m *Master) computeFinalStats() {
	interactions := m.countInteractions()

	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.summary.Interactions = interactions
	m.summary.TotalTestTime = time.Since(m.startTime)
}

func (m *Master) handleMsgDuringTesting(msg slaveRequest) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	slave, exists := m.slaves[msg.slave.ID]
	if !exists {
		msg.errc <- fmt.Errorf("unrecognised slave ID")
		return nil
	}
	if slave.State == slaveFailed || slave.State == slaveFinished {
		msg.errc <- fmt.Errorf("cannot modify slave state once failed or finished")
		return nil
	}

	var err error
	switch msg.slave.State {
	case slaveAccepted:
		msg.errc <- fmt.Errorf("invalid slave state")
		return nil

	case slaveFailed:
		err = NewError(ErrSlaveFailed, nil, "slave failed")
	}
	// if it's still testing (a progress update), finished or failed
	msg.errc <- nil
	slave.update(&msg.slave)
	return err
}

func (m *Master) done() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, slave := range m.slaves {
		if slave.State == slaveTesting || slave.State == slaveAccepted {
			return false
		}
	}
	return true
}

func (m *Master) addSlave(slave *remoteSlave) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if _, exists := m.slaves[slave.ID]; !exists {
		if len(m.slaves) == m.cfg.Master.ExpectSlaves {
			return fmt.Errorf("too many slaves")
		}
		if slave.State != slaveAccepted {
			return fmt.Errorf("slave must be accepted before it can change state")
		}
		m.slaves[slave.ID] = slave
		m.logger.Info("Added slave", "slaveID", slave.ID)
		return nil
	}

	switch slave.State {
	case slaveAccepted:
		return fmt.Errorf("slave already exists")

	case slaveTesting:
		return &stillWaitingForSlaves{}

	case slaveFailed:
		delete(m.slaves, slave.ID)
		m.logger.Info("Removed failed slave", "slaveID", slave.ID)
		return nil
	}
	return fmt.Errorf("invalid state")
}

func (m *Master) ready() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return len(m.slaves) == m.cfg.Master.ExpectSlaves
}

// NOTE: Not thread-safe
func (m *Master) broadcastReady() {
	for _, ch := range m.readySubscribers {
		ch <- true
	}
}

func (m *Master) shutdown() {
	m.logger.Info("Shutting down")
	if err := m.svr.shutdown(); err != nil {
		m.logger.Error("Failed to shut down HTTP server", "err", err)
	}
	if err := m.getShutdownErr(); err != nil {
		m.logger.Error("Master failed", "err", err)
	} else {
		m.logger.Info("Master shut down successfully")
	}
}

func (m *Master) fail(err error) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.flagKill = true
	m.shutdownErr = err
	return err
}

// Kill signals to the master that it must be killed. This occurs
// asynchronously, resulting in the termination of the `Run` method.
func (m *Master) Kill() {
	m.logger.Info("Killing master node")
	m.fail(NewError(ErrMasterFailed, nil, "master was killed")) // nolint: errcheck
}

func (m *Master) killed() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.flagKill
}

func (m *Master) getShutdownErr() error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.shutdownErr
}

func (m *Master) trackStartTime() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.startTime = time.Now()
}

func (m *Master) getSummary() *Summary {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	summaryCopy := *m.summary
	return &summaryCopy
}

func (m *Master) getExpectedInteractions() int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.expectedInteractions
}

func (m *Master) authenticateSlaveRequest(w http.ResponseWriter, r *http.Request) error {
	if !m.cfg.Master.Auth.Enabled {
		return nil
	}
	username, password, ok := r.BasicAuth()
	if !ok {
		return fmt.Errorf("failed to parse basic auth from HTTP header")
	}
	// validate the username
	if username != m.cfg.Master.Auth.Username {
		return fmt.Errorf("invalid username")
	}
	return bcrypt.CompareHashAndPassword([]byte(m.cfg.Master.Auth.PasswordHash), []byte(password))
}

func (m *Master) handleSlaveRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := m.authenticateSlaveRequest(w, r); err != nil {
		jsonResponse(w, "Authentication failed", http.StatusUnauthorized)
		return
	}
	var slave remoteSlave
	if err := fromJSONReadCloser(r.Body, &slave); err != nil {
		jsonResponse(w, fmt.Sprintf("Failed to parse message body: %v", err), http.StatusBadRequest)
		return
	}

	// pass the message on to the master's event loop to be handled and wait for
	// a response
	req := slaveRequest{
		slave: slave,
		errc:  make(chan error, 1),
	}
	m.slavec <- req
	err := <-req.errc
	if err == nil {
		if req.slave.State == slaveAccepted && m.cfg.TestNetwork.Autodetect.Enabled {
			jsonResponse(w, testNetworkTargets{Targets: m.getTargets()}, http.StatusOK)
			return
		}
		jsonResponse(w, "OK", http.StatusOK)
		return
	}

	m.logger.Debug("Result from slave request", "result", err)

	switch err.(type) {
	case *stillWaitingForTargets:
		jsonResponse(w, "Still waiting for all Tendermint targets to become available", http.StatusNotModified)

	case *stillWaitingForSlaves:
		// facilitate the long polling wait until we're ready
		id, readyc := m.registerReadySubscriber()
		defer m.unregisterReadySubscriber(id)
		select {
		case ready := <-readyc:
			if ready {
				jsonResponse(w, "Ready!", http.StatusOK)
			} else {
				// probably another slave that failed
				jsonResponse(w, "Failed", http.StatusServiceUnavailable)
			}

		case <-time.After(DefaultSlaveLongPollTimeout - (1 * time.Second)):
			jsonResponse(w, "Not ready yet", http.StatusNotModified)
		}

	default:
		jsonResponse(w, fmt.Sprintf("Error: %v", err), http.StatusBadRequest)
	}
}

func (m *Master) registerReadySubscriber() (int, chan bool) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	// find an unused subscriber ID
	id := rand.Int()
	_, exists := m.readySubscribers[id]
	for exists {
		id = rand.Int()
		_, exists = m.readySubscribers[id]
	}

	m.readySubscribers[id] = make(chan bool, 1)
	return id, m.readySubscribers[id]
}

func (m *Master) unregisterReadySubscriber(id int) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	delete(m.readySubscribers, id)
}

//-----------------------------------------------------------------------------

func (e *stillWaitingForTargets) Error() string {
	return "still waiting for required number of Tendermint peers"
}

func (e *stillWaitingForSlaves) Error() string {
	return "still waiting for all slaves to connect"
}
