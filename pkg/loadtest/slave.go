package loadtest

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/AsynkronIT/protoactor-go/actor"
	"github.com/AsynkronIT/protoactor-go/remote"
	uuid "github.com/satori/go.uuid"
	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/interchainio/tm-load-test/pkg/loadtest/messages"
)

// Slave is an actor that facilitates load testing. It initially needs to
// connect to a master node to register itself, and then when the master node
// gives the signal it will kick off the load testing.
type Slave struct {
	cfg    *Config
	probe  Probe
	logger logging.Logger

	clientFactory ClientFactory
	master        *actor.PID
	shuttingDown  bool

	checkinTicker *time.Ticker
	stopCheckin   chan bool

	mtx *sync.Mutex
}

// Slave implements actor.Actor
var _ actor.Actor = (*Slave)(nil)

// NewSlave will instantiate a new slave node with the given configuration.
func NewSlave(cfg *Config, probe Probe) (*actor.PID, *actor.RootContext, error) {
	remote.Start(cfg.Slave.Bind)
	ctx := actor.EmptyRootContext
	props := actor.PropsFromProducer(func() actor.Actor {
		return &Slave{
			cfg:           cfg,
			probe:         probe,
			logger:        logging.NewLogrusLogger("slave"),
			clientFactory: GetClientFactory(cfg.Clients.Type),
			master:        actor.NewPID(cfg.Slave.Master, "master"),
			shuttingDown:  false,
			checkinTicker: nil,
			stopCheckin:   make(chan bool, 1),
			mtx:           &sync.Mutex{},
		}
	})
	pid, err := ctx.SpawnNamed(props, uuid.NewV4().String())
	if err != nil {
		return nil, nil, NewError(ErrFailedToCreateActor, err)
	}
	return pid, ctx, nil
}

// Receive handles incoming messages to the slave node.
func (s *Slave) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		s.onStartup(ctx)

	case *actor.Stopped:
		s.onStopped(ctx)

	case *messages.MasterFailed:
		s.shutdown(ctx, NewError(ErrMasterFailed, nil, msg.Reason))

	case *messages.SlaveRejected:
		s.shutdown(ctx, NewError(ErrSlaveFailed, nil, fmt.Sprintf("Slave was rejected: %s", msg.Reason)))

	case *messages.SlaveAccepted:
		s.slaveAccepted(ctx)

	case *messages.StartLoadTest:
		s.startLoadTest(ctx)

	case *messages.SlaveUpdate:
		s.slaveUpdate(ctx, msg)

	case *messages.SlaveFinished:
		s.slaveFinished(ctx, msg)

	case *messages.SlaveFailed:
		s.slaveFailed(ctx, msg)

	case *messages.Kill:
		s.kill(ctx)
	}
}

func (s *Slave) onStartup(ctx actor.Context) {
	s.logger.Info("Slave node is starting up", "addr", ctx.Self().String())
	ctx.Send(s.master, &messages.SlaveReady{Sender: ctx.Self()})

	if s.probe != nil {
		s.probe.OnStartup(ctx)
	}
}

func (s *Slave) onStopped(ctx actor.Context) {
	s.logger.Info("Slave node stopped")
	if s.probe != nil {
		s.probe.OnStopped(ctx)
	}
}

func (s *Slave) shutdown(ctx actor.Context, err error) {
	// indicate to the testing goroutine that we're shutting down now to allow
	// for graceful shutdown
	s.stopLoadTest()

	if err != nil {
		s.logger.Error("Shutting down slave node", "err", err)
	} else {
		s.logger.Info("Shutting down slave node")
	}
	if s.probe != nil {
		s.probe.OnShutdown(ctx, err)
	}
	ctx.Self().GracefulStop()
}

func (s *Slave) slaveAccepted(ctx actor.Context) {
	s.logger.Info("Slave accepted - waiting for load testing to start")
}

func (s *Slave) startLoadTest(ctx actor.Context) {
	s.logger.Info("Starting load test")

	go func(ctx_ actor.Context, slavePID *actor.PID) {
		s.doLoadTest(ctx_, slavePID)
		ctx_.Send(slavePID, &messages.SlaveFinished{Sender: ctx_.Self()})
	}(ctx, ctx.Self())

	s.checkinTicker = time.NewTicker(DefaultHealthCheckInterval)
	go s.checkinLoop(ctx)
}

func (s *Slave) checkinLoop(ctx actor.Context) {
loop:
	for {
		select {
		case <-s.checkinTicker.C:
			s.doCheckin(ctx)

		case <-s.stopCheckin:
			break loop
		}
	}
}

func (s *Slave) doCheckin(ctx actor.Context) {
	s.logger.Debug("Checking in with master")
	ctx.Send(s.master, &messages.LoadTestUnderway{Sender: ctx.Self()})
}

func (s *Slave) doLoadTest(ctx actor.Context, slavePID *actor.PID) {
	clientSpawnRate := int(math.Round(s.cfg.Clients.SpawnRate))
	clientSpawnDelay := int64(1)
	if s.cfg.Clients.SpawnRate < 1.0 {
		clientSpawnRate = 1
		clientSpawnDelay = int64(math.Round(float64(1.0) / s.cfg.Clients.SpawnRate))
	}
	clientParams := ClientParams{
		TargetNodes:        s.cfg.TestNetwork.GetTargetRPCURLs(),
		InteractionTimeout: time.Duration(s.cfg.Clients.InteractionTimeout),
		RequestWaitMin:     time.Duration(s.cfg.Clients.RequestWaitMin),
		RequestWaitMax:     time.Duration(s.cfg.Clients.RequestWaitMax),
		RequestTimeout:     time.Duration(s.cfg.Clients.RequestTimeout),
		TotalClients:       0, // we set this to 0 because we have 0 clients initially
	}
	wg := &sync.WaitGroup{}
	s.logger.Info("Starting client spawning", "desiredCount", s.cfg.Clients.Spawn)
	statsc := make(chan *messages.CombinedStats, s.cfg.Clients.Spawn)
	finalStatsc := make(chan *messages.CombinedStats, 1)
	updateStatsc := make(chan int, s.cfg.Clients.Spawn)
	s.spawnClientStatsReceiver(clientParams, int64(s.cfg.Clients.Spawn), statsc, finalStatsc)
	s.spawnSlaveUpdateReceiver(ctx, slavePID, updateStatsc, int64(s.cfg.Clients.Spawn)*int64(s.cfg.Clients.MaxInteractions))

	startTime := time.Now()
	for totalCount := 0; totalCount < s.cfg.Clients.Spawn; {
		// spawn a batch
		spawned := 0
	batchLoop:
		for batchCount := 0; batchCount < clientSpawnRate; batchCount++ {
			if (batchCount + totalCount) >= s.cfg.Clients.Spawn {
				break batchLoop
			}
			s.spawnClient(wg, clientParams, statsc, updateStatsc)
			spawned++
		}
		totalCount += spawned
		s.logger.Info("Spawned clients", "totalCount", totalCount)
		time.Sleep(time.Duration(clientSpawnDelay) * time.Second)
	}
	s.logger.Info("All clients spawned - waiting for load testing to complete")
	wg.Wait()
	totalTestTime := time.Since(startTime)
	// get the combined stats from the stats counter goroutine
	finalStats := <-finalStatsc
	finalStats.TotalTestTime = totalTestTime.Nanoseconds()
	s.logger.Info("Load testing complete")
	LogStats(logging.NewLogrusLogger(""), finalStats)
	// inform the slave about the final statistics
	ctx.Send(slavePID, &messages.SlaveFinished{Sender: slavePID, Stats: finalStats})
}

func (s *Slave) spawnClient(wg *sync.WaitGroup, clientParams ClientParams, statsc chan *messages.CombinedStats, updateStatsc chan int) {
	wg.Add(1)
	go func() {
		c := s.clientFactory.NewClient(clientParams)
		// execute our interactions from this client
	interactionLoop:
		for i := 0; i < s.cfg.Clients.MaxInteractions; i++ {
			c.Interact()
			// check relatively frequently to see if the testing should stop
			if ((i + 1) % 10) == 0 {
				if s.isShuttingDown() {
					break interactionLoop
				}
			}
			updateStatsc <- 1
		}
		// submit the statistics for counting
		statsc <- c.GetStats()
		wg.Done()
	}()
}

func (s *Slave) spawnClientStatsReceiver(clientParams ClientParams, expectedTotalClients int64, statsc, finalStatsc chan *messages.CombinedStats) {
	go func() {
		overallStats := s.clientFactory.NewStats(clientParams)
		statsReceived := int64(0)
	loop:
		for clientStats := range statsc {
			MergeCombinedStats(overallStats, clientStats)
			statsReceived++
			if statsReceived >= expectedTotalClients {
				break loop
			}
		}
		// submit the overall stats for counting
		finalStatsc <- SummarizeCombinedStats(overallStats)
	}()
}

func (s *Slave) spawnSlaveUpdateReceiver(ctx actor.Context, slavePID *actor.PID, updateStatsc chan int, expectedTotalInteractions int64) {
	go func() {
		interactionCount := int64(0)
		lastUpdate := time.Now()
	loop:
		for range updateStatsc {
			interactionCount++

			if interactionCount >= expectedTotalInteractions {
				break loop
			}

			if time.Since(lastUpdate) >= time.Duration(s.cfg.Slave.UpdateInterval) {
				ctx.Send(slavePID, &messages.SlaveUpdate{Sender: slavePID, InteractionCount: interactionCount})
				lastUpdate = time.Now()
			}
		}
	}()
}

func (s *Slave) stopLoadTest() {
	s.mtx.Lock()
	s.shuttingDown = true
	s.mtx.Unlock()

	// stop the checkin loop
	s.stopCheckin <- true
}

func (s *Slave) isShuttingDown() bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.shuttingDown
}

func (s *Slave) slaveUpdate(ctx actor.Context, msg *messages.SlaveUpdate) {
	s.logger.Info("Interactions completed", "count", msg.InteractionCount)
	ctx.Send(s.master, msg)
}

func (s *Slave) slaveFinished(ctx actor.Context, msg *messages.SlaveFinished) {
	s.logger.Info("Slave successfully finished load testing")
	// forward the message on to the master
	ctx.Send(s.master, msg)
	s.shutdown(ctx, nil)
}

func (s *Slave) slaveFailed(ctx actor.Context, msg *messages.SlaveFailed) {
	s.logger.Error("Slave failed", "reason", msg.Reason)
	// forward the message to the master
	ctx.Send(s.master, msg)
	s.shutdown(ctx, NewError(ErrSlaveFailed, nil, msg.Reason))
}

func (s *Slave) kill(ctx actor.Context) {
	s.logger.Error("Slave killed")
	ctx.Send(s.master, &messages.SlaveFailed{Sender: ctx.Self(), Reason: "Slave killed"})
	s.shutdown(ctx, NewError(ErrKilled, nil))
}
