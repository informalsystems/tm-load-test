package loadtest

import (
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"

	"github.com/AsynkronIT/protoactor-go/actor"
	"github.com/AsynkronIT/protoactor-go/remote"
	"github.com/interchainio/tm-load-test/pkg/loadtest/messages"
)

// Master is an actor that coordinates and collects information from the slaves,
// which are responsible for the actual load testing.
type Master struct {
	cfg    *Config
	probe  Probe
	logger logging.Logger

	slaves *actor.PIDSet

	loadTestStartTime    time.Time               // Helps with calculating overall load testing time for absolute statistics
	istats               *messages.CombinedStats // For keeping track of interaction and request-related statistics
	pstats               *PrometheusStats        // For keeping track of stats from Prometheus
	interactionCount     map[string]int64        // A mapping of slave IDs to interaction counts
	expectedInteractions int64
	lastProgressUpdate   time.Time

	lastCheckin            map[string]time.Time // The last time we received a "load testing underway" message from each slave (ID -> last checkin time)
	checkinTicker          *time.Ticker
	stopSlaveHealthChecker chan bool

	statsShutdownc chan bool
	statsDonec     chan bool

	mtx *sync.Mutex
}

// Master implements actor.Actor
var _ actor.Actor = (*Master)(nil)

// NewMaster will instantiate a new master node. On success, returns an actor
// PID with which one can interact with the master node. On failure, returns an
// error.
func NewMaster(cfg *Config, probe Probe) (*actor.PID, *actor.RootContext, error) {
	remote.Start(cfg.Master.Bind)
	ctx := actor.EmptyRootContext
	props := actor.PropsFromProducer(func() actor.Actor {
		return &Master{
			cfg:    cfg,
			probe:  probe,
			logger: logging.NewLogrusLogger("master"),
			slaves: actor.NewPIDSet(),
			istats: GetClientFactory(cfg.Clients.Type).NewStats(ClientParams{
				TargetNodes:        cfg.TestNetwork.GetTargetRPCURLs(),
				InteractionTimeout: time.Duration(cfg.Clients.InteractionTimeout),
				RequestTimeout:     time.Duration(cfg.Clients.RequestTimeout),
				RequestWaitMin:     time.Duration(cfg.Clients.RequestWaitMin),
				RequestWaitMax:     time.Duration(cfg.Clients.RequestWaitMax),
			}),
			pstats:                 NewPrometheusStats(logging.NewLogrusLogger("prometheus")),
			interactionCount:       make(map[string]int64),
			expectedInteractions:   int64(cfg.Master.ExpectSlaves * cfg.Clients.Spawn * cfg.Clients.MaxInteractions),
			lastProgressUpdate:     time.Now(),
			lastCheckin:            make(map[string]time.Time),
			checkinTicker:          nil,
			stopSlaveHealthChecker: make(chan bool, 1),
			mtx:                    &sync.Mutex{},
			statsShutdownc:         make(chan bool, 1),
			statsDonec:             make(chan bool, 1),
		}
	})
	pid, err := ctx.SpawnNamed(props, "master")
	if err != nil {
		return nil, nil, NewError(ErrFailedToCreateActor, err)
	}
	return pid, ctx, nil
}

// Receive handles incoming messages to the master node.
func (m *Master) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		m.onStartup(ctx)

	case *actor.Stopped:
		m.onStopped(ctx)

	case *messages.CheckAllSlavesConnected:
		m.checkAllSlavesConnected(ctx, msg)

	case *messages.SlaveReady:
		m.slaveReady(ctx, msg)

	case *messages.SlaveFailed:
		m.slaveFailed(ctx, msg)

	case *messages.LoadTestUnderway:
		m.trackSlaveCheckin(msg.Sender.Id)

	case *messages.SlaveUpdate:
		m.slaveUpdate(ctx, msg)

	case *messages.SlaveFinished:
		m.slaveFinished(ctx, msg)

	case *messages.MasterFailed:
		m.masterFailed(ctx, msg)

	case *messages.Kill:
		m.kill(ctx)
	}
}

func (m *Master) onStartup(ctx actor.Context) {
	m.logger.Info("Starting up master node", "addr", ctx.Self().String())
	go func(ctx_ actor.Context) {
		time.Sleep(time.Duration(m.cfg.Master.ExpectSlavesWithin))
		ctx_.Send(ctx_.Self(), &messages.CheckAllSlavesConnected{})
	}(ctx)
	// fire up our Prometheus collector routine
	go func() {
		m.pstats.RunCollectors(m.cfg, m.statsShutdownc, m.statsDonec, m.logger)
	}()
	if m.probe != nil {
		m.probe.OnStartup(ctx)
	}
}

func (m *Master) onStopped(ctx actor.Context) {
	m.logger.Info("Master node stopped")
	if m.probe != nil {
		m.probe.OnStopped(ctx)
	}
}

func (m *Master) checkAllSlavesConnected(ctx actor.Context, msg *messages.CheckAllSlavesConnected) {
	if m.slaves.Len() != m.cfg.Master.ExpectSlaves {
		m.logger.Error("Timed out waiting for all slaves to connect", "slaveCount", m.slaves.Len(), "expected", m.cfg.Master.ExpectSlaves)
		m.broadcast(ctx, &messages.MasterFailed{
			Sender: ctx.Self(),
			Reason: "Timed out waiting for all slaves to connect",
		})
		m.shutdown(ctx, NewError(ErrTimedOutWaitingForSlaves, nil))
	} else {
		m.logger.Debug("All slaves connected within timeout limit - no need to terminate master")
	}
}

func (m *Master) slaveReady(ctx actor.Context, msg *messages.SlaveReady) {
	slave := msg.Sender
	slaveID := slave.String()
	m.logger.Info("Got SlaveReady message", "id", slaveID)
	// keep track of this new incoming slave
	if m.slaves.Contains(slave) {
		m.logger.Error("Already seen slave before - rejecting", "id", slaveID)
		ctx.Send(slave, &messages.SlaveRejected{
			Sender: ctx.Self(),
			Reason: "Already seen slave",
		})
	} else {
		// keep track of the slave
		m.slaves.Add(slave)
		m.logger.Info("Added incoming slave", "slaveCount", m.slaves.Len(), "expected", m.cfg.Master.ExpectSlaves)
		m.trackSlaveCheckin(slave.Id)
		// tell the slave it's got the go-ahead
		ctx.Send(slave, &messages.SlaveAccepted{Sender: ctx.Self()})
		// if we have enough slaves to start the load testing
		if m.slaves.Len() == m.cfg.Master.ExpectSlaves {
			m.startLoadTest(ctx)
		}
	}
}

func (m *Master) startLoadTest(ctx actor.Context) {
	m.logger.Info("Accepted enough connected slaves - starting load test", "slaveCount", m.slaves.Len())
	m.broadcast(ctx, &messages.StartLoadTest{Sender: ctx.Self()})
	m.checkinTicker = time.NewTicker(DefaultHealthCheckInterval)

	// track the start time for the load test
	m.mtx.Lock()
	m.loadTestStartTime = time.Now()
	m.mtx.Unlock()

	go m.slaveHealthChecker(ctx)
}

func (m *Master) slaveHealthChecker(ctx actor.Context) {
loop:
	for {
		select {
		case <-m.checkinTicker.C:
			m.checkSlaveHealth(ctx)

		case <-m.stopSlaveHealthChecker:
			break loop
		}
	}
}

func (m *Master) checkSlaveHealth(ctx actor.Context) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// we're only interested in slaves that are still connected
	for _, slave := range m.slaves.Values() {
		slaveID := slave.Id
		lastSeen := m.lastCheckin[slaveID]
		if time.Since(lastSeen) >= DefaultMaxMissedHealthCheckPeriod {
			m.logger.Error("Failed to see slave recently enough", "slaveID", slaveID, "lastSeen", lastSeen.String())
			ctx.Send(ctx.Self(), &messages.MasterFailed{
				Sender: ctx.Self(),
				Reason: fmt.Sprintf("Failed to see slave %s within %s", slaveID, DefaultMaxMissedHealthCheckPeriod.String()),
			})
			// don't bother checking any of the other slaves (plus this will
			// most likely result in bombardment of the slaves with MasterFailed
			// messages)
			return
		}
	}
}

func (m *Master) masterFailed(ctx actor.Context, msg *messages.MasterFailed) {
	// rebroadcast this message to the slaves
	m.broadcast(ctx, msg)
	// we're done here
	m.shutdown(ctx, NewError(ErrSlaveFailed, nil))
}

func (m *Master) trackSlaveCheckin(slaveID string) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.lastCheckin[slaveID] = time.Now()
}

func (m *Master) broadcast(ctx actor.Context, msg interface{}) {
	m.logger.Debug("Broadcasting message to all slaves", "msg", msg)
	m.slaves.ForEach(func(i int, pid actor.PID) {
		m.logger.Debug("Broadcasting message to slave", "pid", pid)
		ctx.Send(&pid, msg)
	})
}

func fmtTimeLeft(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02dh%02dm%02ds", h, m, s)
}

func (m *Master) slaveUpdate(ctx actor.Context, msg *messages.SlaveUpdate) {
	m.updateSlaveInteractionCount(msg.Sender.Id, msg.InteractionCount)

	if m.dueForProgressUpdate() {
		totalInteractions := m.totalSlaveInteractionCount()
		completed := float64(100) * (float64(totalInteractions) / float64(m.expectedInteractions))
		interactionsPerSec := float64(totalInteractions) / time.Since(m.loadTestStartTime).Seconds()
		timeLeft := time.Duration(1000 * time.Hour)
		if interactionsPerSec > 0 {
			timeLeft = time.Duration((float64(m.expectedInteractions-totalInteractions) / interactionsPerSec) * float64(time.Second))
		}
		m.logger.Info(
			"Progress update",
			"completed", fmt.Sprintf("%.2f%%", completed),
			"interactionsPerSec", fmt.Sprintf("%.2f", interactionsPerSec),
			"approxTimeLeft", fmtTimeLeft(timeLeft),
		)
		m.progressUpdated()
	}
}

func (m *Master) updateSlaveInteractionCount(slaveID string, count int64) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.interactionCount[slaveID] = count
}

func (m *Master) totalSlaveInteractionCount() int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	totalCount := int64(0)
	for _, count := range m.interactionCount {
		totalCount += count
	}
	return totalCount
}

func (m *Master) dueForProgressUpdate() bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	return time.Since(m.lastProgressUpdate) >= (20 * time.Second)
}

func (m *Master) progressUpdated() {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.lastProgressUpdate = time.Now()
}

func (m *Master) slaveFailed(ctx actor.Context, msg *messages.SlaveFailed) {
	slave := msg.Sender
	slaveID := slave.String()
	m.logger.Error("Slave failed", "id", slaveID, "reason", msg.Reason)
	m.slaves.Remove(slave)
	m.broadcast(ctx, &messages.MasterFailed{Sender: ctx.Self(), Reason: "One other attached slave failed"})
	m.shutdown(ctx, NewError(ErrSlaveFailed, nil))
}

func (m *Master) slaveFinished(ctx actor.Context, msg *messages.SlaveFinished) {
	slave := msg.Sender
	slaveID := slave.String()
	m.logger.Info("Slave finished", "id", slaveID)
	m.updateStats(msg.Stats)
	m.slaves.Remove(slave)
	// if we've heard from all the slaves we accepted
	if m.slaves.Len() == 0 {
		m.logger.Info("All slaves successfully completed their load testing")
		m.trackTestEndTime()
		if err := m.sanityCheckStats(); err != nil {
			m.logger.Error("Statistics sanity check failed", "err", err)
			m.shutdown(ctx, err)
		} else {
			m.shutdown(ctx, nil)
		}
	}
}

func (m *Master) trackTestEndTime() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.istats.TotalTestTime = time.Since(m.loadTestStartTime).Nanoseconds()
	m.logger.Debug("Tracking total test duration", "TotalTestTime", m.istats.TotalTestTime)
}

func (m *Master) kill(ctx actor.Context) {
	m.logger.Error("Master killed")
	m.broadcast(ctx, &messages.MasterFailed{Sender: ctx.Self(), Reason: "Master killed"})
	m.shutdown(ctx, NewError(ErrKilled, nil))
}

func (m *Master) stopPrometheusCollectors() {
	m.statsShutdownc <- true
	select {
	case <-m.statsDonec:
		m.logger.Debug("Prometheus collectors successfully shut down")
	case <-time.After(30 * time.Second):
		m.logger.Error("Timed out waiting for Prometheus collectors to shut down")
	}
}

func (m *Master) shutdown(ctx actor.Context, err error) {
	// stop checking slave health
	m.stopSlaveHealthChecker <- true
	m.stopPrometheusCollectors()
	m.writeStats()
	if err != nil {
		m.logger.Error("Shutting down master node", "err", err)
	} else {
		m.logger.Info("Shutting down master node")
	}
	if m.probe != nil {
		m.probe.OnShutdown(ctx, err)
	}
	ctx.Self().GracefulStop()
}

func (m *Master) updateStats(stats *messages.CombinedStats) {
	m.mtx.Lock()
	MergeCombinedStats(m.istats, stats)
	m.mtx.Unlock()
}

func (m *Master) sanityCheckStats() error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	expectedClients := m.cfg.Master.ExpectSlaves * m.cfg.Clients.Spawn
	if m.istats.Interactions.TotalClients != int64(expectedClients) {
		return NewError(
			ErrStatsSanityCheckFailed,
			nil,
			fmt.Sprintf("Expected total clients to be %d, but was %d", expectedClients, m.istats.Interactions.TotalClients),
		)
	}

	expectedInteractions := expectedClients * m.cfg.Clients.MaxInteractions
	if m.istats.Interactions.Count != int64(expectedInteractions) {
		return NewError(
			ErrStatsSanityCheckFailed,
			nil,
			fmt.Sprintf("Expected total interactions to be %d, but was %d", expectedInteractions, m.istats.Interactions.Count),
		)
	}

	return nil
}

func (m *Master) writeStats() {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	LogStats(logging.NewLogrusLogger(""), m.istats)
	filename := path.Join(m.cfg.Master.ResultsDir, "summary.csv")
	if err := WriteCombinedStatsToFile(filename, m.istats); err != nil {
		m.logger.Error("Failed to write final statistics to output CSV file", "filename", filename)
	}
	if err := m.pstats.Dump(m.cfg.Master.ResultsDir); err != nil {
		m.logger.Error("Failed to write Prometheus stats to output directory", "err", err)
	}
}
