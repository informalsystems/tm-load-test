package loadtest

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/interchainio/tm-load-test/pkg/loadtest/messages"
)

// Run must be executed from your `main` function in your Go code. This can be
// used to fast-track the construction of your own load testing tool for your
// Tendermint ABCI application.
func Run() {
	var (
		isMaster   = flag.Bool("master", false, "start this process in MASTER mode")
		isSlave    = flag.Bool("slave", false, "start this process in SLAVE mode")
		configFile = flag.String("c", "load-test.toml", "the path to the configuration file for a load test")
		isVerbose  = flag.Bool("v", false, "increase logging verbosity to DEBUG level")
	)
	flag.Usage = func() {
		fmt.Println(`Tendermint load testing utility

tm-load-test is a tool for distributed load testing on Tendermint networks,
assuming that your Tendermint network currently runs the "kvstore" proxy app.

Usage:
  tm-load-test -c load-test.toml -master   # Run a master
  tm-load-test -c load-test.toml -slave    # Run a slave

Flags:`)
		flag.PrintDefaults()
		fmt.Println("")
	}
	flag.Parse()

	if (!*isMaster && !*isSlave) || (*isMaster && *isSlave) {
		fmt.Println("Either -master or -slave is expected on the command line to explicitly specify which mode to use.")
		os.Exit(1)
	}

	if *isVerbose {
		logrus.SetLevel(logrus.DebugLevel)
	}
	logger := logrus.WithField("ctx", "main")

	var err error

	if *isMaster {
		logger.Infoln("Starting in MASTER mode")
		err = RunMaster(*configFile)
	} else {
		logger.Infoln("Starting in SLAVE mode")
		err = RunSlave(*configFile)
	}

	if err != nil {
		logger.WithError(err).Errorln("Load test execution failed")
		if ltErr, ok := err.(*Error); ok {
			os.Exit(int(ltErr.Code))
		} else {
			os.Exit(1)
		}
	}
}

// RunMaster will build and execute a master node for load testing and will
// block until the testing is complete or fails.
func RunMaster(configFile string) error {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return err
	}
	return RunMasterWithConfig(cfg)
}

// RunMasterWithConfig runs a master node with the given configuration and
// blocks until the testing is complete or it fails.
func RunMasterWithConfig(cfg *Config) error {
	probe := NewStandardProbe()
	mpid, ctx, err := NewMaster(cfg, probe)
	if err != nil {
		return err
	}
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-sigc
		ctx.Send(mpid, &messages.Kill{})
	}()
	// wait for the master node to terminate
	return probe.Wait()
}

// RunSlave will build and execute a slave node for load testing and will block
// until the testing is complete or fails.
func RunSlave(configFile string) error {
	cfg, err := LoadConfig(configFile)
	if err != nil {
		return err
	}
	return RunSlaveWithConfig(cfg)
}

// RunSlaveWithConfig runs a slave node with the given configuration and blocks
// until the testing is complete or it fails.
func RunSlaveWithConfig(cfg *Config) error {
	probe := NewStandardProbe()
	spid, ctx, err := NewSlave(cfg, probe)
	if err != nil {
		return err
	}
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-sigc
		ctx.Send(spid, &messages.Kill{})
	}()
	return probe.Wait()
}
