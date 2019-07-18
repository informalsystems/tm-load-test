package loadtest

import (
	"os"
	"os/signal"
	"syscall"

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
	cancelTrap := trapInterrupts(tg, logger)
	defer close(cancelTrap)

	if err := tg.Wait(); err != nil {
		logger.Error("Failed to execute load test", "err", err)
		return err
	}
	logger.Info("Load test complete!")
	return nil
}

func trapInterrupts(tg *TransactorGroup, logger logging.Logger) chan struct {} {
	sigc := make(chan os.Signal, 1)
	cancelTrap := make(chan struct{})
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigc:
			logger.Info("Caught kill signal")
			tg.Cancel()
		case <-cancelTrap:
			return
		}
	}()
	return cancelTrap
}
