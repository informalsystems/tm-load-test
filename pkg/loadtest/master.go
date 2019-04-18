package loadtest

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// DefaultMasterCheckinInterval is the interval at which the master makes sure
// that all slaves have checked in.
const DefaultMasterCheckinInterval = DefaultSlaveUpdateInterval / 3

// Master is an actor that coordinates and collects information from the slaves,
// which are responsible for the actual load testing.
type Master struct {
	cfg    *Config
	logger logging.Logger

	svr *http.Server // The primary master server HTTP interface (for metrics and slave endpoints).

	// For high-level progress tracking
	loadTestStartTime    time.Time
	loadTestEndTime      time.Time
	loadTestDuration     time.Duration
	expectedInteractions int64

	readyCheckTicker *time.Ticker // For checking whether the master's ready to go
	checkinTicker    *time.Ticker

	wg sync.WaitGroup // Wait group for managing the HTTP server shutdown process.

	mtx              sync.Mutex
	flagStarted      bool                 // Flag to indicate whether or not the HTTP server's been started.
	flagReady        bool                 // Flag that's set to true when the master is ready to start load testing.
	flagKill         bool                 // Flag that's set to true when the master is to be killed.
	interactionCount map[string]int64     // A mapping of slave IDs to interaction counts
	lastCheckin      map[string]time.Time // The last time we received a "load testing underway" message from each slave (ID -> last checkin time)
}

// NewMaster instantiates a master node, ready to be executed.
func NewMaster(cfg *Config) (*Master, error) {
	logger := logging.NewLogrusLogger("master")
	logger.Debug("Creating master")
	master := &Master{
		cfg:                  cfg,
		logger:               logger,
		expectedInteractions: int64(cfg.Clients.MaxInteractions) * int64(cfg.Clients.Spawn),
		interactionCount:     make(map[string]int64),
		lastCheckin:          make(map[string]time.Time),
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/slave/", master.handleSlaveRequest)
	master.svr = &http.Server{
		Addr:    cfg.Master.Bind,
		Handler: mux,
	}
	return master, nil
}

func (m *Master) handleSlaveRequest(w http.ResponseWriter, req *http.Request) {

}

// Run executes the entirety of the master's operation in the current goroutine,
// and only returns once the load testing is done (or has failed).
func (m *Master) Run() error {
	defer m.shutdown()
	return m.run()
}

func (m *Master) run() error {
	if err := m.startHTTPServer(); err != nil {
		return err
	}
	if err := m.waitForSlaves(); err != nil {
		return err
	}
	if err := m.startLoadTesting(); err != nil {
		return err
	}
	return nil
}

func (m *Master) startHTTPServer() error {
	errc := make(chan error, 1)
	m.wg.Add(1)
	go func() {
		if err := m.svr.ListenAndServe(); err != nil {
			m.logger.Error("Failed to start master HTTP server", "err", err)
			errc <- err
		} else {
			m.logger.Info("Successfully shut down master HTTP server")
		}
		m.wg.Done()
	}()

	// wait for the server to start
	select {
	case err := <-errc:
		return err

	case <-time.After(100 * time.Millisecond):
	}
	m.setStarted()
	m.logger.Info("Started master HTTP server", "addr", m.svr.Addr)
	return nil
}

func (m *Master) shutdownHTTPServer() {
	if !m.hasStarted() {
		return
	}

	defer m.wg.Wait()
	if err := m.svr.Shutdown(context.Background()); err != nil {
		m.logger.Error("Failed to gracefully shut down HTTP server", "err", err)
	}
}

func (m *Master) shutdown() {
	m.logger.Info("Shutting down")
	m.shutdownHTTPServer()
}

func (m *Master) waitForSlaves() error {
	return nil
}

func (m *Master) startLoadTesting() error {
	return nil
}

// Kill attempts to gracefully kill the master node, allowing it to shut down
// properly before terminating. This is an asynchronous process, however, and
// the master will still only effectively finish terminating once the Run
// function completes.
func (m *Master) Kill() {
	m.logger.Info("Killing master...")
	m.setKill()
}

func (m *Master) setKill() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.flagKill = true
}

func (m *Master) mustKill() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.flagKill
}

func (m *Master) setStarted() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.flagStarted = true
}

func (m *Master) hasStarted() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.flagStarted
}

// func (m *Master) onStartup(ctx actor.Context) {
// 	m.logger.Info("Starting up master node", "addr", ctx.Self().String())
// 	go func(ctx_ actor.Context) {
// 		time.Sleep(time.Duration(m.cfg.Master.ExpectSlavesWithin))
// 		ctx_.Send(ctx_.Self(), &messages.CheckAllSlavesConnected{})
// 	}(ctx)
// 	if m.probe != nil {
// 		m.probe.OnStartup(ctx)
// 	}
// }

// func (m *Master) onStopped(ctx actor.Context) {
// 	m.logger.Info("Master node stopped")
// 	if m.probe != nil {
// 		m.probe.OnStopped(ctx)
// 	}
// }

// func (m *Master) checkAllSlavesConnected(ctx actor.Context, msg *messages.CheckAllSlavesConnected) {
// 	if m.slaves.Len() != m.cfg.Master.ExpectSlaves {
// 		m.logger.Error("Timed out waiting for all slaves to connect", "slaveCount", m.slaves.Len(), "expected", m.cfg.Master.ExpectSlaves)
// 		m.broadcast(ctx, &messages.MasterFailed{
// 			Sender: ctx.Self(),
// 			Reason: "Timed out waiting for all slaves to connect",
// 		})
// 		m.shutdown(ctx, NewError(ErrTimedOutWaitingForSlaves, nil))
// 	} else {
// 		m.logger.Debug("All slaves connected within timeout limit - no need to terminate master")
// 	}
// }

// func (m *Master) slaveReady(ctx actor.Context, msg *messages.SlaveReady) {
// 	slave := msg.Sender
// 	slaveID := slave.String()
// 	m.logger.Info("Got SlaveReady message", "id", slaveID)
// 	// keep track of this new incoming slave
// 	if m.slaves.Contains(slave) {
// 		m.logger.Error("Already seen slave before - rejecting", "id", slaveID)
// 		ctx.Send(slave, &messages.SlaveRejected{
// 			Sender: ctx.Self(),
// 			Reason: "Already seen slave",
// 		})
// 	} else {
// 		// keep track of the slave
// 		m.slaves.Add(slave)
// 		m.logger.Info("Added incoming slave", "slaveCount", m.slaves.Len(), "expected", m.cfg.Master.ExpectSlaves)
// 		m.trackSlaveCheckin(slave.Id)
// 		// tell the slave it's got the go-ahead
// 		ctx.Send(slave, &messages.SlaveAccepted{Sender: ctx.Self()})
// 		// if we have enough slaves to start the load testing
// 		if m.slaves.Len() == m.cfg.Master.ExpectSlaves {
// 			m.startLoadTest(ctx)
// 		}
// 	}
// }

// func (m *Master) startLoadTest(ctx actor.Context) {
// 	m.logger.Info("Accepted enough connected slaves - starting load test", "slaveCount", m.slaves.Len())
// 	m.broadcast(ctx, &messages.StartLoadTest{Sender: ctx.Self()})
// 	m.checkinTicker = time.NewTicker(DefaultHealthCheckInterval)

// 	// track the start time for the load test
// 	m.mtx.Lock()
// 	m.loadTestStartTime = time.Now()
// 	m.mtx.Unlock()

// 	go m.slaveHealthChecker(ctx)
// }

// func (m *Master) slaveHealthChecker(ctx actor.Context) {
// loop:
// 	for {
// 		select {
// 		case <-m.checkinTicker.C:
// 			m.checkSlaveHealth(ctx)

// 		case <-m.stopSlaveHealthChecker:
// 			break loop
// 		}
// 	}
// }

// func (m *Master) checkSlaveHealth(ctx actor.Context) {
// 	m.mtx.Lock()
// 	defer m.mtx.Unlock()

// 	// we're only interested in slaves that are still connected
// 	for _, slave := range m.slaves.Values() {
// 		slaveID := slave.Id
// 		lastSeen := m.lastCheckin[slaveID]
// 		if time.Since(lastSeen) >= DefaultMaxMissedHealthCheckPeriod {
// 			m.logger.Error("Failed to see slave recently enough", "slaveID", slaveID, "lastSeen", lastSeen.String())
// 			ctx.Send(ctx.Self(), &messages.MasterFailed{
// 				Sender: ctx.Self(),
// 				Reason: fmt.Sprintf("Failed to see slave %s within %s", slaveID, DefaultMaxMissedHealthCheckPeriod.String()),
// 			})
// 			// don't bother checking any of the other slaves (plus this will
// 			// most likely result in bombardment of the slaves with MasterFailed
// 			// messages)
// 			return
// 		}
// 	}
// }

// func (m *Master) masterFailed(ctx actor.Context, msg *messages.MasterFailed) {
// 	// rebroadcast this message to the slaves
// 	m.broadcast(ctx, msg)
// 	// we're done here
// 	m.shutdown(ctx, NewError(ErrSlaveFailed, nil))
// }

// func (m *Master) trackSlaveCheckin(slaveID string) {
// 	m.mtx.Lock()
// 	defer m.mtx.Unlock()
// 	m.lastCheckin[slaveID] = time.Now()
// }

// func (m *Master) broadcast(ctx actor.Context, msg interface{}) {
// 	m.logger.Debug("Broadcasting message to all slaves", "msg", msg)
// 	m.slaves.ForEach(func(i int, pid actor.PID) {
// 		m.logger.Debug("Broadcasting message to slave", "pid", pid)
// 		ctx.Send(&pid, msg)
// 	})
// }

// func fmtTimeLeft(d time.Duration) string {
// 	d = d.Round(time.Second)
// 	h := d / time.Hour
// 	d -= h * time.Hour
// 	m := d / time.Minute
// 	d -= m * time.Minute
// 	s := d / time.Second
// 	return fmt.Sprintf("%02dh%02dm%02ds", h, m, s)
// }

// func (m *Master) slaveUpdate(ctx actor.Context, msg *messages.SlaveUpdate) {
// 	m.updateSlaveInteractionCount(msg.Sender.Id, msg.InteractionCount)

// 	if m.dueForProgressUpdate() {
// 		totalInteractions := m.totalSlaveInteractionCount()
// 		completed := float64(100) * (float64(totalInteractions) / float64(m.expectedInteractions))
// 		interactionsPerSec := float64(totalInteractions) / time.Since(m.loadTestStartTime).Seconds()
// 		timeLeft := time.Duration(1000 * time.Hour)
// 		if interactionsPerSec > 0 {
// 			timeLeft = time.Duration((float64(m.expectedInteractions-totalInteractions) / interactionsPerSec) * float64(time.Second))
// 		}
// 		m.logger.Info(
// 			"Progress update",
// 			"completed", fmt.Sprintf("%.2f%%", completed),
// 			"interactionsPerSec", fmt.Sprintf("%.2f", interactionsPerSec),
// 			"approxTimeLeft", fmtTimeLeft(timeLeft),
// 		)
// 		m.progressUpdated()
// 	}
// }

// func (m *Master) updateSlaveInteractionCount(slaveID string, count int64) {
// 	m.mtx.Lock()
// 	defer m.mtx.Unlock()
// 	m.interactionCount[slaveID] = count
// }

// func (m *Master) totalSlaveInteractionCount() int64 {
// 	m.mtx.Lock()
// 	defer m.mtx.Unlock()

// 	totalCount := int64(0)
// 	for _, count := range m.interactionCount {
// 		totalCount += count
// 	}
// 	return totalCount
// }

// func (m *Master) dueForProgressUpdate() bool {
// 	m.mtx.Lock()
// 	defer m.mtx.Unlock()

// 	return time.Since(m.lastProgressUpdate) >= (20 * time.Second)
// }

// func (m *Master) progressUpdated() {
// 	m.mtx.Lock()
// 	defer m.mtx.Unlock()

// 	m.lastProgressUpdate = time.Now()
// }

// func (m *Master) slaveFailed(ctx actor.Context, msg *messages.SlaveFailed) {
// 	slave := msg.Sender
// 	slaveID := slave.String()
// 	m.logger.Error("Slave failed", "id", slaveID, "reason", msg.Reason)
// 	m.slaves.Remove(slave)
// 	m.broadcast(ctx, &messages.MasterFailed{Sender: ctx.Self(), Reason: "One other attached slave failed"})
// 	m.shutdown(ctx, NewError(ErrSlaveFailed, nil))
// }

// func (m *Master) slaveFinished(ctx actor.Context, msg *messages.SlaveFinished) {
// 	slave := msg.Sender
// 	slaveID := slave.String()
// 	m.logger.Info("Slave finished", "id", slaveID)
// 	m.slaves.Remove(slave)
// 	// if we've heard from all the slaves we accepted
// 	if m.slaves.Len() == 0 {
// 		m.logger.Info("All slaves successfully completed their load testing")
// 		m.trackTestEndTime()
// 		if err := m.sanityCheckStats(); err != nil {
// 			m.logger.Error("Statistics sanity check failed", "err", err)
// 			m.shutdown(ctx, err)
// 		} else {
// 			m.shutdown(ctx, nil)
// 		}
// 	}
// }

// func (m *Master) trackTestEndTime() {
// 	m.mtx.Lock()
// 	defer m.mtx.Unlock()
// 	m.loadTestEndTime = time.Now()
// 	m.loadTestDuration = m.loadTestEndTime.Sub(m.loadTestStartTime)
// 	m.logger.Debug("Tracking total test duration", "TotalTestTime", m.loadTestDuration)
// }

// func (m *Master) kill(ctx actor.Context) {
// 	m.logger.Error("Master killed")
// 	m.broadcast(ctx, &messages.MasterFailed{Sender: ctx.Self(), Reason: "Master killed"})
// 	m.shutdown(ctx, NewError(ErrKilled, nil))
// }

// func (m *Master) shutdown(ctx actor.Context, err error) {
// 	// stop checking slave health
// 	m.stopSlaveHealthChecker <- true
// 	if err != nil {
// 		m.logger.Error("Shutting down master node", "err", err)
// 	} else {
// 		m.logger.Info("Shutting down master node")
// 	}
// 	if m.probe != nil {
// 		m.probe.OnShutdown(ctx, err)
// 	}
// 	ctx.Self().GracefulStop()
// }

// func (m *Master) sanityCheckStats() error {
// 	expectedClients := m.cfg.Master.ExpectSlaves * m.cfg.Clients.Spawn
// 	expectedInteractions := expectedClients * m.cfg.Clients.MaxInteractions
// 	totalInteractions := m.totalInteractions()
// 	if totalInteractions != int64(expectedInteractions) {
// 		return NewError(
// 			ErrStatsSanityCheckFailed,
// 			nil,
// 			fmt.Sprintf("Expected total interactions to be %d, but was %d", expectedInteractions, totalInteractions),
// 		)
// 	}

// 	return nil
// }

// func (m *Master) totalInteractions() int64 {
// 	m.mtx.Lock()
// 	defer m.mtx.Unlock()

// 	total := int64(0)
// 	for _, count := range m.interactionCount {
// 		total += count
// 	}
// 	return total
// }
