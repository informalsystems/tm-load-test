package loadtest

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest/clients"
	"github.com/interchainio/tm-load-test/pkg/timeutils"

	"github.com/BurntSushi/toml"
)

// DefaultHealthCheckInterval is the interval at which slave nodes are expected
// to send a `LoadTestUnderway` message after starting their load testing.
const DefaultHealthCheckInterval = 10 * time.Second

// DefaultMaxMissedHealthChecks is the number of health checks that a slave can
// miss before being considered as "failed" (i.e. one more missed health check
// than `DefaultMaxMissedHealthChecks` will result in total load testing
// failure).
const DefaultMaxMissedHealthChecks = 2

// DefaultMaxMissedHealthCheckPeriod is the time after which a slave is
// considered to have failed if we don't hear from it during that period.
const DefaultMaxMissedHealthCheckPeriod = ((DefaultMaxMissedHealthChecks + 1) * DefaultHealthCheckInterval)

// Config is the central configuration structure for our load testing, from both
// the master and slaves' perspectives.
type Config struct {
	Master      MasterConfig      `toml:"master"`       // The master's load testing configuration.
	Slave       SlaveConfig       `toml:"slave"`        // The slaves' load testing configuration.
	TestNetwork TestNetworkConfig `toml:"test_network"` // The test network layout/configuration.
	Clients     clients.Config    `toml:"clients"`      // Load testing client-related configuration.
}

// MasterConfig provides the configuration for the load testing master.
type MasterConfig struct {
	Bind               string                      `toml:"bind"`                 // The address to which to bind the master (host:port).
	ExpectSlaves       int                         `toml:"expect_slaves"`        // The number of slaves to expect to connect before starting the load test.
	ExpectSlavesWithin timeutils.ParseableDuration `toml:"expect_slaves_within"` // The time period within which to expect to hear from all slaves, otherwise causes a failure.
}

// SlaveConfig provides configuration specific to the load testing slaves.
type SlaveConfig struct {
	Bind               string                      `toml:"bind"`   // The address to which to bind slave nodes (host:port).
	Master             string                      `toml:"master"` // The master's external address (host:port).
	ExpectMasterWithin timeutils.ParseableDuration `toml:"expect_master_within"`
	ExpectStartWithin  timeutils.ParseableDuration `toml:"expect_start_within"`
}

// TestNetworkConfig encapsulates information about the network under test.
type TestNetworkConfig struct {
	Autodetect TestNetworkAutodetectConfig `toml:"autodetect"`
	Targets    []TestNetworkTargetConfig   `toml:"targets"` // Configuration for each of the Tendermint nodes in the network.
}

// TestNetworkAutodetectConfig encapsulates information relating to the
// autodetection of the Tendermint test network nodes under test.
type TestNetworkAutodetectConfig struct {
	Enabled             bool                        `toml:"enabled"`        // Is target network autodetection enabled?
	SeedNode            string                      `toml:"seed_node"`      // The seed node from which to find other peers/targets.
	ExpectTargets       int                         `toml:"expect_targets"` // The number of targets to expect prior to starting load testing.
	ExpectTargetsWithin timeutils.ParseableDuration `toml:"expect_targets_within"`
}

// TestNetworkTargetConfig encapsulates the configuration for each node in the
// Tendermint test network.
type TestNetworkTargetConfig struct {
	ID  string `toml:"id"`  // A short, descriptive identifier for this node.
	URL string `toml:"url"` // The RPC URL for this target node.

	Outages string `toml:"outages,omitempty"` // Specify an outage schedule to try to affect for this host.
}

// ParseConfig will parse the configuration from the given string.
func ParseConfig(data string) (*Config, error) {
	var cfg Config
	if _, err := toml.Decode(data, &cfg); err != nil {
		return nil, NewError(ErrFailedToDecodeConfig, err)
	}
	// validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfig will attempt to load configuration from the given file.
func LoadConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, NewError(ErrFailedToReadConfigFile, err)
	}
	return ParseConfig(string(data))
}

//
// Config
//

// Validate does a deep check on the configuration to make sure it makes sense.
func (c *Config) Validate() error {
	if err := c.Master.Validate(); err != nil {
		return err
	}
	if err := c.Slave.Validate(); err != nil {
		return err
	}
	if err := c.TestNetwork.Validate(); err != nil {
		return err
	}
	if err := c.Clients.Validate(); err != nil {
		return err
	}
	return nil
}

//
// MasterConfig
//

func (m *MasterConfig) Validate() error {
	if len(m.Bind) == 0 {
		return NewError(ErrInvalidConfig, nil, "master bind address must be specified")
	}
	if m.ExpectSlaves < 1 {
		return NewError(ErrInvalidConfig, nil, "master must expect at least one slave")
	}
	return nil
}

//
// SlaveConfig
//

func (s *SlaveConfig) Validate() error {
	if len(s.Bind) == 0 {
		return NewError(ErrInvalidConfig, nil, "slave needs non-empty bind address")
	}
	if len(s.Master) == 0 {
		return NewError(ErrInvalidConfig, nil, "slave address for master must be explicitly specified")
	}
	return nil
}

//
// TestNetworkConfig
//

func (c *TestNetworkConfig) Validate() error {
	// if we're autodetecting the network, no need to validate any explicit test
	// network config
	if c.Autodetect.Enabled {
		return c.Autodetect.Validate()
	}

	if len(c.Targets) == 0 {
		return NewError(ErrInvalidConfig, nil, "test network must have at least one target (found 0)")
	}
	for i, target := range c.Targets {
		if err := target.Validate(i); err != nil {
			return err
		}
	}
	return nil
}

//
// TestNetworkAutodetectConfig
//

func (c *TestNetworkAutodetectConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.SeedNode) == 0 {
		return NewError(ErrInvalidConfig, nil, "test network autodetection requires a seed node address, but none provided")
	}
	if c.ExpectTargets <= 0 {
		return NewError(ErrInvalidConfig, nil, "test network autodetection requires at least 1 expected target to start testing")
	}
	return nil
}

// GetTargetRPCURLs will return a simple, flattened list of URLs for all of the
// target nodes' RPC addresses.
func (c *TestNetworkConfig) GetTargetRPCURLs() []string {
	urls := make([]string, 0)
	for _, target := range c.Targets {
		urls = append(urls, target.URL)
	}
	return urls
}

//
// TestNetworkTargetConfig
//

func (c *TestNetworkTargetConfig) Validate(i int) error {
	if len(c.ID) == 0 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("test network target %d is missing an ID", i))
	}
	if len(c.URL) == 0 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("test network target %d is missing its RPC URL", i))
	}
	return nil
}
