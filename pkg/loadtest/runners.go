package loadtest

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/interchainio/tm-load-test/pkg/loadtest/clients"

	"github.com/sirupsen/logrus"
)

// Here we register all of the built-in producers.
func init() {
	clients.RegisterClientType("kvstore-http", clients.NewKVStoreHTTPClientType())
	clients.RegisterClientType("kvstore-websockets", clients.NewKVStoreWebSocketsClientType())
}

// Run must be executed from your `main` function in your Go code. This can be
// used to fast-track the construction of your own load testing tool for your
// Tendermint ABCI application.
func Run() {
	var (
		rootCmd    = flag.NewFlagSet("root", flag.ExitOnError)
		mode       = rootCmd.String("mode", "", "the mode in which to start tm-load-test; can be master/slave/standalone")
		configFile = rootCmd.String("c", "load-test.toml", "the path to the configuration file for a load test")
		isVerbose  = rootCmd.Bool("v", false, "increase logging verbosity to DEBUG level")
	)
	rootCmd.Usage = func() {
		fmt.Println(`Tendermint load testing utility

tm-load-test is a tool for distributed load testing on Tendermint networks,
assuming that your Tendermint network currently runs the "kvstore" proxy app.

Usage (master/slave):
  tm-load-test -c load-test.toml -mode master   # Run a master
  tm-load-test -c load-test.toml -mode slave    # Run a slave

Usage (standalone):
  tm-load-test -c load-test.toml -mode standalone # Execute a single master and slave node together

Flags:`)
		rootCmd.PrintDefaults()
		fmt.Println("")
	}
	if err := rootCmd.Parse(os.Args[1:]); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	if *isVerbose {
		logrus.SetLevel(logrus.DebugLevel)
	}
	logger := logging.NewLogrusLogger("main")

	var summary *Summary
	var err error

	switch *mode {
	case "master":
		logger.Info("Starting in MASTER mode")
		summary, err = RunMaster(*configFile)

	case "slave":
		logger.Info("Starting in SLAVE mode")
		summary, err = RunSlave(*configFile)

	case "standalone":
		logger.Info("Starting in STANDALONE mode")
		summary, err = RunStandalone(*configFile)

	default:
		logger.Error("Invalid mode for starting load testing tool", "mode", *mode)
		os.Exit(1)
	}

	if err != nil {
		logger.Error("Load test execution failed", "err", err)
		if ltErr, ok := err.(*Error); ok {
			os.Exit(int(ltErr.Code))
		} else {
			os.Exit(1)
		}
	}
	summary.Log(logger)
}

// RunMaster will build and execute a master node for load testing and will
// block until the testing is complete or fails.
func RunMaster(configFile string) (*Summary, error) {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return nil, err
	}
	return RunMasterWithConfig(cfg)
}

// RunMasterWithConfig runs a master node with the given configuration and
// blocks until the testing is complete or it fails.
func RunMasterWithConfig(cfg *Config) (*Summary, error) {
	master, err := NewMaster(cfg)
	if err != nil {
		return nil, err
	}
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	go func() {
		sig := <-sigc
		master.logger.Info("Received signal", "signal", sig.String())
		master.Kill()
	}()
	// wait for the master node to terminate
	return master.Run()
}

// RunSlave will build and execute a slave node for load testing and will block
// until the testing is complete or fails.
func RunSlave(configFile string) (*Summary, error) {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return nil, err
	}
	return RunSlaveWithConfig(cfg)
}

// RunSlaveWithConfig runs a slave node with the given configuration and blocks
// until the testing is complete or it fails.
func RunSlaveWithConfig(cfg *Config) (*Summary, error) {
	slave, err := NewSlave(cfg)
	if err != nil {
		return nil, err
	}
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	go func() {
		sig := <-sigc
		slave.logger.Info("Received signal", "signal", sig.String())
		slave.Kill()
	}()
	return slave.Run()
}

// RunStandalone executes a master and slave node together on the same machine,
// returning the summary and final status from the master component of the
// standalone load test operation.
func RunStandalone(configFile string) (summary *Summary, err error) {
	var cfg *Config
	cfg, err = LoadConfig(configFile)
	if err != nil {
		return
	}
	// override whatever number of clients we expect
	cfg.Master.ExpectSlaves = 1
	// resolve the master's bind address
	cfg.Master.Bind, err = resolveBindAddr(cfg.Master.Bind)
	if err != nil {
		return
	}

	// make sure the slave knows this precise address
	var u *url.URL
	u, err = url.Parse(fmt.Sprintf("http://%s", cfg.Master.Bind))
	if err != nil {
		err = NewError(ErrInvalidConfig, err, "failed to construct internal master address for standalone mode")
		return
	}
	cfg.Slave.Master = ParseableURL(*u)

	var master *Master
	var slave *Slave

	master, err = NewMaster(cfg)
	if err != nil {
		return
	}
	slave, err = NewSlave(cfg)
	if err != nil {
		return
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	go func() {
		sig := <-sigc
		master.logger.Info("Received signal", "signal", sig.String())
		master.Kill()
		slave.Kill()
	}()

	masterDonec := make(chan struct{})
	slaveDonec := make(chan struct{})

	go func() {
		summary, err = master.Run()
		close(masterDonec)
	}()
	go func() {
		_, _ = slave.Run()
		close(slaveDonec)
	}()

	// wait for both to finish
	<-masterDonec
	<-slaveDonec
	return
}
