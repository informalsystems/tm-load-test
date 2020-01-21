package loadtest

import (
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
)

// ExecuteStandalone will run a standalone (non-master/slave) load test.
func ExecuteStandalone(cfg Config) error {
	logger := logging.NewLogrusLogger("loadtest")

	// if we need to wait for the network to stabilize first
	if cfg.ExpectPeers > 0 {
		peers, err := waitForTendermintNetworkPeers(
			cfg.Endpoints,
			cfg.EndpointSelectMethod,
			cfg.ExpectPeers,
			cfg.MinConnectivity,
			cfg.MaxEndpoints,
			time.Duration(cfg.PeerConnectTimeout)*time.Second,
			logger,
		)
		if err != nil {
			logger.Error("Failed while waiting for peers to connect", "err", err)
			return err
		}
		cfg.Endpoints = peers
	}

	logger.Info("Connecting to remote endpoints")
	tg := NewTransactorGroup()
	if err := tg.AddAll(&cfg); err != nil {
		return err
	}
	logger.Info("Initiating load test")
	tg.Start()

	var cancelTrap chan struct{}
	if !cfg.NoTrapInterrupts {
		// we want to know if the user hits Ctrl+Break
		cancelTrap = trapInterrupts(func() { tg.Cancel() }, logger)
		defer close(cancelTrap)
	} else {
		logger.Debug("Skipping trapping of interrupts (e.g. Ctrl+Break)")
	}

	if err := tg.Wait(); err != nil {
		logger.Error("Failed to execute load test", "err", err)
		return err
	}

	// if we need to write the final statistics
	if len(cfg.StatsOutputFile) > 0 {
		logger.Info("Writing aggregate statistics", "outputFile", cfg.StatsOutputFile)
		if err := tg.WriteAggregateStats(cfg.StatsOutputFile); err != nil {
			logger.Error("Failed to write aggregate statistics", "err", err)
			return err
		}
	}

	logger.Info("Load test complete!")
	return nil
}
