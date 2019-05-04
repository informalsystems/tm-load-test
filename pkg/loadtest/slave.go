package loadtest

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/interchainio/tm-load-test/pkg/loadtest/clients"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	cmn "github.com/tendermint/tendermint/libs/common"
)

// DefaultSlaveLongPollTimeout determines our long-polling interval when
// interacting with the master during registration and when waiting for testing
// to start.
const DefaultSlaveLongPollTimeout = 30 * time.Second

// DefaultSlaveUpdateTimeout indicates the timeout when sending interim updates
// to the master.
const DefaultSlaveUpdateTimeout = 3 * time.Second

// DefaultSlaveClientsKillMaxWait is the maximum time a slave will wait once
// it's sent the kill signal to its clients for all of them to shut down.
const DefaultSlaveClientsKillMaxWait = 30 * time.Second

type slaveState string

const (
	slaveAccepted slaveState = "accepted"
	slaveTesting  slaveState = "testing"
	slaveFinished slaveState = "finished"
	slaveFailed   slaveState = "failed"
)

// Slave is an agent that facilitates load testing. It initially needs to
// connect to a master node to register itself, and then when the master node
// gives the signal it will kick off the load testing.
type Slave struct {
	cfg    *Config
	logger logging.Logger

	clientFactory     clients.Factory // Spawns the clients during load testing.
	maxInteractions   int64           // The maximum possible number of interactions summed across all clients (for progress reporting).
	interactionsc     chan int64      // Receives an interaction count after each interaction to allow for counting of interactions and updating progress.
	interactionsStopc chan struct{}   // Closed when we need to stop counting interactions.

	prometheus *baseServer

	mtx             sync.Mutex
	id              string
	flagKill        bool      // Set to true when the slave must be killed.
	flagKillClients bool      // Set to true when the clients must be killed (doesn't necessarily mean the slave must be killed).
	startTime       time.Time // When the testing was started.
	interactions    int64     // A count of all of the interactions executed so far.
}

// NewSlave will instantiate a new slave node with the given configuration.
func NewSlave(cfg *Config) (*Slave, error) {
	slaveID := generateSlaveID()
	logger := logging.NewLogrusLogger("slave-" + slaveID)
	logger.Debug("Creating slave")
	// We need to instantiate clientFactory before the Prometheus server
	// otherwise the Prometheus metrics won't have been registered yet
	clientFactory := clients.GetClientType(cfg.Clients.Type).NewFactory(
		cfg.Clients,
		getOrGenerateHostID(slaveID),
		cfg.TestNetwork.GetTargetRPCURLs(),
	)
	// instantiate the Prometheus metrics server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	prometheus, err := newBaseServer(cfg.Slave.Bind, mux, logger)
	if err != nil {
		return nil, err
	}

	maxInteractions := int64(cfg.Clients.Spawn) * int64(cfg.Clients.MaxInteractions)
	if maxInteractions <= 0 {
		return nil, fmt.Errorf("maximum number of interactions (clients.spawn * clients.max_interactions) must be greater than zero")
	}
	logger.Debug("Slave stop criteria", "maxInteractions", maxInteractions)
	return &Slave{
		cfg:               cfg,
		logger:            logger,
		id:                slaveID,
		clientFactory:     clientFactory,
		maxInteractions:   maxInteractions,
		interactionsc:     make(chan int64, cfg.Clients.Spawn),
		interactionsStopc: make(chan struct{}),
		prometheus:        prometheus,
	}, err
}

func (s *Slave) getID() string {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.id
}

func (s *Slave) mustKill() bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.flagKill
}

func (s *Slave) setKill() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.flagKill = true
	// we also need to kill the clients
	s.flagKillClients = true
}

func (s *Slave) mustKillClients() bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.flagKillClients
}

func (s *Slave) setKillClients() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.flagKillClients = true
}

func (s *Slave) addInteractions(c int64) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.interactions += c
}

func (s *Slave) getInteractions() int64 {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.interactions
}

func (s *Slave) trackTestStart() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.startTime = time.Now()
}

func (s *Slave) getStartTime() time.Time {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.startTime
}

// Runs the overall load testing operation, keeping track of the state as it
// goes.
func (s *Slave) run() error {
	if err := s.register(); err != nil {
		return err
	}
	if err := s.waitToStart(); err != nil {
		return s.failAndUpdateStateWithMaster(err)
	}
	if err := s.doLoadTest(); err != nil {
		return s.failAndUpdateStateWithMaster(err)
	}
	return nil
}

func (s *Slave) failAndUpdateStateWithMaster(err error) error {
	if e := s.updateStateWithMaster(slaveFailed, err.Error()); e != nil {
		s.logger.Error("Failed to send state update to master", "err", e)
	}
	// do a pass-through of the error
	return err
}

// Does long polling to try to eventually convince the master to change the
// state of this slave.
func (s *Slave) updateStateWithMaster(state slaveState, status string) error {
	rs := remoteSlave{
		ID:           s.getID(),
		State:        state,
		Status:       status,
		Interactions: s.getInteractions(),
	}
	// tell the master we want to set the state for this slave
	msg, err := toJSON(&rs)
	if err != nil {
		return NewError(ErrSlaveFailed, err, "failed to marshal JSON message")
	}

	reqURL := fmt.Sprintf("%s/slave", s.cfg.Slave.Master)
	s.logger.Debug("Long polling master", "url", reqURL, "state", rs.State, "interactions", rs.Interactions)
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(msg))
	if err != nil {
		return NewError(ErrSlaveFailed, err, "failed to construct request")
	}
	req.Header.Set("Content-Type", "application/json")

	_, err = longPoll(
		req,
		DefaultSlaveLongPollTimeout,
		s.cfg.Slave.ExpectMasterWithin.Duration(),
		s.mustKill,
		s.logger,
	)
	return err
}

// Send an update to the master to tell it how far this slave is with its load
// testing. Doesn't do any long polling.
func (s *Slave) sendProgressToMaster() error {
	interactionCount := s.getInteractions()
	msg, err := toJSON(remoteSlave{ID: s.getID(), State: slaveTesting, Interactions: interactionCount})
	if err != nil {
		return fmt.Errorf("failed to marshal slave update message: %v", err)
	}
	reqURL := fmt.Sprintf("%s/slave", s.cfg.Slave.Master)
	progress := fmt.Sprintf("%.1f%%", float64(100)*(float64(interactionCount)/float64(s.maxInteractions)))
	s.logger.Info("Sending progress update to master", "url", reqURL, "count", interactionCount, "progress", progress)

	client := &http.Client{
		Timeout: DefaultSlaveUpdateTimeout,
	}
	res, err := client.Post(reqURL, "application/json", strings.NewReader(msg))
	if err != nil {
		return fmt.Errorf("failed to POST slave update message: %v", err)
	} else if res.StatusCode != 200 {
		return fmt.Errorf("got unexpected status code from master: %d", res.StatusCode)
	}
	return nil
}

// Keeps trying to register with the master until it succeeds or times out.
func (s *Slave) register() error {
	s.logger.Info("Attempting to register with master")
	return s.updateStateWithMaster(slaveAccepted, "Slave registered with master")
}

// Keeps polling the master to see when it's time to start the load testing.
func (s *Slave) waitToStart() error {
	s.logger.Info("Polling master to check if ready")
	return s.updateStateWithMaster(slaveTesting, "Slave has started load testing")
}

// Does the actual load test execution.
func (s *Slave) doLoadTest() error {
	rate, delay := s.cfg.Clients.SpawnRateAndDelay()
	s.logger.Info("Starting load test", "rate", rate, "delay", delay)

	// count interactions asynchronously from all the clients
	go s.countInteractions()
	defer close(s.interactionsStopc)

	s.trackTestStart()
	var wg sync.WaitGroup
	// spawn all of our clients in batches
	for totalSpawned := int64(0); totalSpawned < int64(s.cfg.Clients.Spawn); totalSpawned += int64(rate) {
		toSpawn := int64(rate)
		// make sure we only spawn precisely the required number of clients
		if (totalSpawned + int64(toSpawn)) > int64(s.cfg.Clients.Spawn) {
			toSpawn = int64(s.cfg.Clients.Spawn) - totalSpawned
		}
		// spawn a batch
		s.spawnClientBatch(toSpawn, &wg)
		// wait a bit before spawning the next batch, but allow for the slave to
		// be killed here
		if s.mustKill() {
			s.killClientsAndWait(&wg, nil)
			return s.failAndUpdateStateWithMaster(fmt.Errorf("slave killed"))
		}
		time.Sleep(delay)
	}
	return s.waitForClientsToFinish(&wg, s.getStartTime(), s.cfg.Clients.MaxTestTime.Duration())
}

func (s *Slave) spawnClientBatch(count int64, wg *sync.WaitGroup) {
	s.logger.Info("Spawning client batch", "count", count)
	for i := int64(0); i < count; i++ {
		s.spawnClient(wg)
	}
}

func (s *Slave) waitForClientsToFinish(wg *sync.WaitGroup, startTime time.Time, testTimeLeft time.Duration) error {
	donec := make(chan struct{})
	go func() {
		defer close(donec)
		wg.Wait()
	}()
loop:
	for {
		// if the slave's being killed
		if s.mustKill() {
			s.logger.Info("Slave kill signal received")
			s.killClientsAndWait(nil, donec)
			return s.updateStateWithMaster(slaveFailed, "Slave killed")
		}
		testTime := time.Since(startTime)
		if testTime > testTimeLeft {
			s.logger.Info("Load test completed: maximum test time reached", "time", testTime)
			s.killClientsAndWait(nil, donec)
			break loop
		}
		select {
		case <-donec:
			s.logger.Info("Load test completed: maximum interactions executed", "interactions", s.maxInteractions)
			break loop
		case <-time.After(1 * time.Second):
		}
	}
	return s.updateStateWithMaster(slaveFinished, "Slave has completed load testing")
}

func (s *Slave) killClientsAndWait(wg *sync.WaitGroup, donec chan struct{}) {
	s.setKillClients()
	if wg != nil {
		wg.Wait()
	} else if donec != nil {
		select {
		case <-donec:
		case <-time.After(DefaultSlaveClientsKillMaxWait):
		}
	}
}

// Spawns a single client and executes all of its interactions.
func (s *Slave) spawnClient(wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		c := s.clientFactory.NewClient()
	interactionLoop:
		for i := 0; i < s.cfg.Clients.MaxInteractions; i++ {
			c.Interact()
			if s.mustKillClients() {
				break interactionLoop
			}
			s.interactionsc <- 1
		}
		wg.Done()
	}()
}

func (s *Slave) countInteractions() {
	updateTicker := time.NewTicker(DefaultHealthCheckInterval)
	defer updateTicker.Stop()
loop:
	for {
		select {
		case <-updateTicker.C:
			s.sendProgressToMasterAndTrack()

		case c := <-s.interactionsc:
			s.addInteractions(c)

		case <-s.interactionsStopc:
			break loop
		}
	}
}

func (s *Slave) sendProgressToMasterAndTrack() {
	if err := s.sendProgressToMaster(); err != nil {
		s.logger.Error("Failed to send progress update to master", "err", err)
		s.Kill()
	}
}

// Run executes the slave's entire process in the current goroutine.
func (s *Slave) Run() (*Summary, error) {
	defer s.shutdown()
	if err := s.prometheus.start(); err != nil {
		s.updateStateWithMaster(slaveFailed, err.Error()) // nolint: errcheck
		return nil, err
	}
	if err := s.run(); err != nil {
		s.updateStateWithMaster(slaveFailed, err.Error()) // nolint: errcheck
		return nil, err
	}
	return &Summary{
		Interactions:  s.getInteractions(),
		TotalTestTime: time.Since(s.getStartTime()),
	}, nil
}

// Kill will attempt to kill the slave (gracefully). This executes
// asynchronously and returns immediately. The Run method will only conclude
// once the shutdown is complete.
func (s *Slave) Kill() {
	s.logger.Info("Killing slave...")
	s.setKill()
}

func (s *Slave) shutdown() {
	s.logger.Info("Shutting down")
	if err := s.prometheus.shutdown(); err != nil {
		s.logger.Error("Failed to shut down Prometheus server", "err", err)
	} else {
		s.logger.Info("Bye!")
	}
}

func generateSlaveID() string {
	return cmn.RandStr(8)
}
