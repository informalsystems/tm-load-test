package loadtest

import (
	"encoding"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"

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
	Clients     ClientConfig      `toml:"clients"`      // Load testing client-related configuration.
}

// MasterConfig provides the configuration for the load testing master.
type MasterConfig struct {
	Bind               string            `toml:"bind"`                 // The address to which to bind the master (host:port).
	ExpectSlaves       int               `toml:"expect_slaves"`        // The number of slaves to expect to connect before starting the load test.
	ExpectSlavesWithin ParseableDuration `toml:"expect_slaves_within"` // The time period within which to expect to hear from all slaves, otherwise causes a failure.
	ResultsDir         string            `toml:"results_dir"`          // The root of the results output directory.
}

// SlaveConfig provides configuration specific to the load testing slaves.
type SlaveConfig struct {
	Bind           string            `toml:"bind"`            // The address to which to bind slave nodes (host:port).
	Master         string            `toml:"master"`          // The master's external address (host:port).
	UpdateInterval ParseableDuration `toml:"update_interval"` // The interval with which to send stats updates to the master.
}

// TestNetworkConfig encapsulates information about the network under test.
type TestNetworkConfig struct {
	EnablePrometheus       bool              `toml:"enable_prometheus"`        // Should we enable collections of Prometheus stats during testing?
	PrometheusPollInterval ParseableDuration `toml:"prometheus_poll_interval"` // How often should we poll the Prometheus endpoint?
	PrometheusPollTimeout  ParseableDuration `toml:"prometheus_poll_timeout"`  // At what point do we consider a Prometheus polling operation a failure?

	Targets []TestNetworkTargetConfig `toml:"targets"` // Configuration for each of the Tendermint nodes in the network.
}

// TestNetworkTargetConfig encapsulates the configuration for each node in the
// Tendermint test network.
type TestNetworkTargetConfig struct {
	ID             string `toml:"id"`                        // A short, descriptive identifier for this node.
	URL            string `toml:"url"`                       // The RPC URL for this target node.
	PrometheusURLs string `toml:"prometheus_urls,omitempty"` // The URL(s) (comma-separated) to poll for this target node, if Prometheus polling is enabled.

	Outages string `toml:"outages,omitempty"` // Specify an outage schedule to try to affect for this host.
}

// ClientConfig contains the configuration for clients being spawned on slaves.
type ClientConfig struct {
	Type               string            `toml:"type"`                // The type of client to spawn.
	Spawn              int               `toml:"spawn"`               // The number of clients to spawn, per slave.
	SpawnRate          float64           `toml:"spawn_rate"`          // The rate at which to spawn clients, per second, on each slave.
	MaxInteractions    int               `toml:"max_interactions"`    // The maximum number of interactions emanating from each client.
	MaxTestTime        ParseableDuration `toml:"max_test_time"`       // The maximum duration of the test, beyond which this client must be stopped.
	RequestWaitMin     ParseableDuration `toml:"request_wait_min"`    // The minimum wait period before each request before sending another one.
	RequestWaitMax     ParseableDuration `toml:"request_wait_max"`    // The maximum wait period before each request before sending another one.
	RequestTimeout     ParseableDuration `toml:"request_timeout"`     // The maximum time allowed before considering a request to have timed out.
	InteractionTimeout ParseableDuration `toml:"interaction_timeout"` // The maximum time allowed for an overall interaction.
}

// ParseableDuration represents a time.Duration that implements
// encoding.TextUnmarshaler.
type ParseableDuration time.Duration

// PrometheusEndpoint is what we parse from the `prometheus_urls` parameter in
// the target network config for each node.
type PrometheusEndpoint struct {
	ID  string
	URL string
}

// ParseableDuration implements encoding.TextUnmarshaler
var _ encoding.TextUnmarshaler = (*ParseableDuration)(nil)

var configLogger = logging.NewLogrusLogger("config")

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

// UnmarshalText allows us a convenient way to unmarshal durations.
func (d *ParseableDuration) UnmarshalText(text []byte) error {
	dur, err := time.ParseDuration(string(text))
	if err == nil {
		*d = ParseableDuration(dur)
	}
	return err
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
	if len(m.ResultsDir) == 0 {
		return NewError(ErrInvalidConfig, nil, "master results output directory must be specified")
	}
	return nil
}

//
// SlaveConfig
//

func (s *SlaveConfig) Validate() error {
	if len(s.Master) == 0 {
		return NewError(ErrInvalidConfig, nil, "slave address for master must be explicitly specified")
	}
	if s.UpdateInterval == 0 {
		return NewError(ErrInvalidConfig, nil, "slave update interval must be specified")
	}
	return nil
}

//
// TestNetworkConfig
//

func (c *TestNetworkConfig) Validate() error {
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
	if len(c.PrometheusURLs) == 0 {
		return NewError(ErrInvalidConfig, nil, fmt.Sprintf("test network target %d is missing its Prometheus URL(s)", i))
	}
	return nil
}

// GetPrometheusEndpoints will extract the endpoint info for all of the validly
// specified Prometheus endpoints. The right format for `prometheus_urls`
// parameter resembles the following:
// id1=http://server1,id2=http://server2
func (c TestNetworkTargetConfig) GetPrometheusEndpoints() []*PrometheusEndpoint {
	idURLs := strings.Split(c.PrometheusURLs, ",")
	endpoints := make([]*PrometheusEndpoint, 0)
	for _, idURL := range idURLs {
		parts := strings.Split(idURL, "=")
		if len(parts) > 1 {
			endpoints = append(endpoints, &PrometheusEndpoint{
				ID:  parts[0],
				URL: strings.Join(parts[1:], "="), // in case there's a "=" symbol somewhere in the URL
			})
		}
	}
	return endpoints
}

//
// ClientConfig
//

func (c *ClientConfig) Validate() error {
	if clientFactory := GetClientFactory(c.Type); clientFactory == nil {
		return NewError(
			ErrInvalidConfig,
			nil,
			fmt.Sprintf("client type is unrecognized (supported: %s)", strings.Join(GetSupportedClientFactoryIDs(), ",")),
		)
	}
	if c.Spawn < 1 {
		return NewError(ErrInvalidConfig, nil, "client spawn count must be greater than 0")
	}
	if c.SpawnRate <= 0 {
		return NewError(ErrInvalidConfig, nil, "client spawn rate must be a positive floating point number")
	}
	if c.MaxInteractions == -1 {
		if c.MaxTestTime == 0 {
			return NewError(ErrInvalidConfig, nil, "if client max interactions is -1, max test time must be set")
		}
	} else if c.MaxInteractions < 1 {
		return NewError(ErrInvalidConfig, nil, "client maximum interactions must be -1, or greater than 1")
	}
	if c.RequestTimeout <= 0 {
		return NewError(ErrInvalidConfig, nil, "client request timeout cannot be 0")
	}
	if c.MaxTestTime > 0 {
		configLogger.Info("WARNING: max_text_time parameter is not yet implemented")
	}
	return nil
}
