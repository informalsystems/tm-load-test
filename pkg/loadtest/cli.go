package loadtest

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/informalsystems/tm-load-test/internal/logging"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// CLIVersion must be manually updated as new versions are released.
const CLIVersion = "v0.9.0"

// cliVersionCommitID must be set through linker settings. See
// https://stackoverflow.com/a/11355611/1156132 for details.
var cliVersionCommitID string

// CLIConfig allows developers to customize their own load testing tool.
type CLIConfig struct {
	AppName              string
	AppShortDesc         string
	AppLongDesc          string
	DefaultClientFactory string
}

var (
	flagVerbose bool
)

func buildCLI(cli *CLIConfig, logger logging.Logger) *cobra.Command {
	cobra.OnInitialize(func() { initLogLevel(logger) })
	var cfg Config
	rootCmd := &cobra.Command{
		Use:   cli.AppName,
		Short: cli.AppShortDesc,
		Long:  cli.AppLongDesc,
		Run: func(cmd *cobra.Command, args []string) {
			logger.Debug(fmt.Sprintf("Configuration: %s", cfg.ToJSON()))
			if err := cfg.Validate(); err != nil {
				logger.Error(err.Error())
				os.Exit(1)
			}

			if err := ExecuteStandalone(cfg); err != nil {
				os.Exit(1)
			}
		},
	}
	rootCmd.PersistentFlags().StringVar(&cfg.ClientFactory, "client-factory", cli.DefaultClientFactory, "The identifier of the client factory to use for generating load testing transactions")
	rootCmd.PersistentFlags().IntVarP(&cfg.Connections, "connections", "c", 1, "The number of connections to open to each endpoint simultaneously")
	rootCmd.PersistentFlags().IntVarP(&cfg.Time, "time", "T", 60, "The duration (in seconds) for which to handle the load test")
	rootCmd.PersistentFlags().IntVarP(&cfg.SendPeriod, "send-period", "p", 1, "The period (in seconds) at which to send batches of transactions")
	rootCmd.PersistentFlags().IntVarP(&cfg.Rate, "rate", "r", 1000, "The number of transactions to generate each second on each connection, to each endpoint")
	rootCmd.PersistentFlags().IntVarP(&cfg.Size, "size", "s", 250, "The size of each transaction, in bytes - must be greater than 40")
	rootCmd.PersistentFlags().IntVarP(&cfg.Count, "count", "N", -1, "The maximum number of transactions to send - set to -1 to turn off this limit")
	rootCmd.PersistentFlags().StringVar(&cfg.BroadcastTxMethod, "broadcast-tx-method", "async", "The broadcast_tx method to use when submitting transactions - can be async, sync or commit")
	rootCmd.PersistentFlags().StringSliceVar(&cfg.Endpoints, "endpoints", []string{}, "A comma-separated list of URLs indicating Tendermint WebSockets RPC endpoints to which to connect")
	rootCmd.PersistentFlags().StringVar(&cfg.EndpointSelectMethod, "endpoint-select-method", SelectSuppliedEndpoints, "The method by which to select endpoints")
	rootCmd.PersistentFlags().IntVar(&cfg.ExpectPeers, "expect-peers", 0, "The minimum number of peers to expect when crawling the P2P network from the specified endpoint(s) prior to waiting for slaves to connect")
	rootCmd.PersistentFlags().IntVar(&cfg.MaxEndpoints, "max-endpoints", 0, "The maximum number of endpoints to use for testing, where 0 means unlimited")
	rootCmd.PersistentFlags().IntVar(&cfg.PeerConnectTimeout, "peer-connect-timeout", 600, "The number of seconds to wait for all required peers to connect if expect-peers > 0")
	rootCmd.PersistentFlags().IntVar(&cfg.MinConnectivity, "min-peer-connectivity", 0, "The minimum number of peers to which each peer must be connected before starting the load test")
	rootCmd.PersistentFlags().StringVar(&cfg.StatsOutputFile, "stats-output", "", "Where to store aggregate statistics (in CSV format) for the load test")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Increase output logging verbosity to DEBUG level")

	var masterCfg MasterConfig
	masterCmd := &cobra.Command{
		Use:   "master",
		Short: "Start load test application in MASTER mode",
		Run: func(cmd *cobra.Command, args []string) {
			logger.Debug(fmt.Sprintf("Configuration: %s", cfg.ToJSON()))
			logger.Debug(fmt.Sprintf("Master configuration: %s", masterCfg.ToJSON()))
			if err := cfg.Validate(); err != nil {
				logger.Error(err.Error())
				os.Exit(1)
			}
			if err := masterCfg.Validate(); err != nil {
				logger.Error(err.Error())
				os.Exit(1)
			}
			master := NewMaster(&cfg, &masterCfg)
			if err := master.Run(); err != nil {
				os.Exit(1)
			}
		},
	}
	masterCmd.PersistentFlags().StringVar(&masterCfg.BindAddr, "bind", "localhost:26670", "A host:port combination to which to bind the master on which to listen for slave connections")
	masterCmd.PersistentFlags().IntVar(&masterCfg.ExpectSlaves, "expect-slaves", 2, "The number of slaves to expect to connect to the master before starting load testing")
	masterCmd.PersistentFlags().IntVar(&masterCfg.SlaveConnectTimeout, "connect-timeout", 180, "The maximum number of seconds to wait for all slaves to connect")
	masterCmd.PersistentFlags().IntVar(&masterCfg.ShutdownWait, "shutdown-wait", 0, "The number of seconds to wait after testing completes prior to shutting down the web server")
	masterCmd.PersistentFlags().IntVar(&masterCfg.LoadTestID, "load-test-id", 0, "The ID of the load test currently underway")

	var slaveCfg SlaveConfig
	slaveCmd := &cobra.Command{
		Use:   "slave",
		Short: "Start load test application in SLAVE mode",
		Run: func(cmd *cobra.Command, args []string) {
			logger.Debug(fmt.Sprintf("Slave configuration: %s", slaveCfg.ToJSON()))
			if err := slaveCfg.Validate(); err != nil {
				logger.Error(err.Error())
				os.Exit(1)
			}
			slave, err := NewSlave(&slaveCfg)
			if err != nil {
				logger.Error("Failed to create new slave", "err", err)
				os.Exit(1)
			}
			if err := slave.Run(); err != nil {
				os.Exit(1)
			}
		},
	}
	slaveCmd.PersistentFlags().StringVar(&slaveCfg.ID, "id", "", "An optional unique ID for this slave. Will show up in metrics and logs. If not specified, a UUID will be generated.")
	slaveCmd.PersistentFlags().StringVar(&slaveCfg.MasterAddr, "master", "ws://localhost:26670", "The WebSockets URL on which to find the master node")
	slaveCmd.PersistentFlags().IntVar(&slaveCfg.MasterConnectTimeout, "connect-timeout", 180, "The maximum number of seconds to keep trying to connect to the master")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Display the version of tm-load-test and exit",
		Run: func(cmd *cobra.Command, args []string) {
			version := CLIVersion
			if len(cliVersionCommitID) > 0 {
				version = fmt.Sprintf("%s-%s", version, cliVersionCommitID)
			}
			fmt.Println("tm-load-test", version)
		},
	}

	rootCmd.AddCommand(masterCmd)
	rootCmd.AddCommand(slaveCmd)
	rootCmd.AddCommand(versionCmd)
	return rootCmd
}

func initLogLevel(logger logging.Logger) {
	if flagVerbose {
		logrus.SetLevel(logrus.DebugLevel)
		logger.Debug("Set logging level to DEBUG")
	}
}

// Run must be executed from your `main` function in your Go code. This can be
// used to fast-track the construction of your own load testing tool for your
// Tendermint ABCI application.
func Run(cli *CLIConfig) {
	logger := logging.NewLogrusLogger("main")
	if err := buildCLI(cli, logger).Execute(); err != nil {
		logger.Error("Error", "err", err)
	}
}

func trapInterrupts(onKill func(), logger logging.Logger) chan struct{} {
	sigc := make(chan os.Signal, 1)
	cancelTrap := make(chan struct{})
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigc:
			logger.Info("Caught kill signal")
			onKill()
		case <-cancelTrap:
			logger.Debug("Interrupt trap cancelled")
		}
	}()
	return cancelTrap
}
