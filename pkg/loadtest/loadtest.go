package loadtest

import (
	"github.com/interchainio/tm-load-test/internal/logging"
)

func executeLoadTest(cfg *Config) error {
	logger := logging.NewLogrusLogger("loadtest")
	logger.Info("Connecting to remote endpoints")
	tg := NewTransactorGroup()
	if err := tg.AddAll(cfg); err != nil {
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
