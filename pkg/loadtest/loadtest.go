package loadtest

import (
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
)

func executeLoadTest(cfg Config) error {
	logger := logging.NewLogrusLogger("loadtest")

	// if we need to wait for the network to stabilize first
	if cfg.ExpectPeers > 0 {
		peers, err := waitForTendermintNetworkPeers(
			cfg.Endpoints,
			cfg.ExpectPeers,
			time.Duration(cfg.PeerConnectTimeout)*time.Second,
			logger,
		)
		if err != nil {
			logger.Error("Failed while waiting for peers to connect", "err", err)
			return err
		}
		if cfg.EndpointSelectMethod == SelectCrawledEndpoints {
			cfg.Endpoints = peers
		}
	}

	logger.Info("Connecting to remote endpoints")
	tg := NewTransactorGroup()
	if err := tg.AddAll(&cfg); err != nil {
		return err
	}
	logger.Info("Initiating load test")
	tg.Start()

	// we want to know if the user hits Ctrl+Break
	cancelTrap := trapInterrupts(func() { tg.Cancel() }, logger)
	defer close(cancelTrap)

	if err := tg.Wait(); err != nil {
		logger.Error("Failed to execute load test", "err", err)
		return err
	}
	logger.Info("Load test complete!")
	return nil
}
