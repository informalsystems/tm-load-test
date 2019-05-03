package loadtest

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/interchainio/tm-load-test/pkg/loadtest/clients"

	"github.com/sirupsen/logrus"
)

// Here we register all of the built-in producers.
func init() {
	clients.RegisterFactoryProducer("kvstore-http", clients.NewKVStoreHTTPFactoryProducer())
}

// Run must be executed from your `main` function in your Go code. This can be
// used to fast-track the construction of your own load testing tool for your
// Tendermint ABCI application.
func Run() {
	var (
		rootCmd    = flag.NewFlagSet("root", flag.ExitOnError)
		isMaster   = rootCmd.Bool("master", false, "start this process in MASTER mode")
		isSlave    = rootCmd.Bool("slave", false, "start this process in SLAVE mode")
		configFile = rootCmd.String("c", "load-test.toml", "the path to the configuration file for a load test")
		isVerbose  = rootCmd.Bool("v", false, "increase logging verbosity to DEBUG level")
	)
	rootCmd.Usage = func() {
		fmt.Println(`Tendermint load testing utility

tm-load-test is a tool for distributed load testing on Tendermint networks,
assuming that your Tendermint network currently runs the "kvstore" proxy app.

Usage:
  tm-load-test -c load-test.toml -master   # Run a master
  tm-load-test -c load-test.toml -slave    # Run a slave

Flags:`)
		rootCmd.PrintDefaults()
		fmt.Println("")
	}
	if err := rootCmd.Parse(os.Args[1:]); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	if (!*isMaster && !*isSlave) || (*isMaster && *isSlave) {
		fmt.Println("Either -master or -slave is expected on the command line to explicitly specify which mode to use.")
		os.Exit(1)
	}

	if *isVerbose {
		logrus.SetLevel(logrus.DebugLevel)
	}
	logger := logging.NewLogrusLogger("main")

	var summary *Summary
	var err error

	if *isMaster {
		logger.Info("Starting in MASTER mode")
		summary, err = RunMaster(*configFile)
	} else {
		logger.Info("Starting in SLAVE mode")
		summary, err = RunSlave(*configFile)
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
