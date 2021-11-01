package loadtest

import (
	"encoding/json"
	"fmt"
)

const (
	SelectSuppliedEndpoints   = "supplied"   // Select only the supplied endpoint(s) for load testing (the default).
	SelectDiscoveredEndpoints = "discovered" // Select newly discovered endpoints only (excluding supplied endpoints).
	SelectAnyEndpoints        = "any"        // Select from any of supplied and/or discovered endpoints.
)

var validEndpointSelectMethods = map[string]interface{}{
	SelectSuppliedEndpoints:   nil,
	SelectDiscoveredEndpoints: nil,
	SelectAnyEndpoints:        nil,
}

// Config represents the configuration for a single client (i.e. standalone or
// worker).
type Config struct {
	ClientFactory        string   `json:"client_factory"`         // Which client factory should we use for load testing?
	Connections          int      `json:"connections"`            // The number of WebSockets connections to make to each target endpoint.
	Time                 int      `json:"time"`                   // The total time, in seconds, for which to handle the load test.
	SendPeriod           int      `json:"send_period"`            // The period (in seconds) at which to send batches of transactions.
	Rate                 int      `json:"rate"`                   // The number of transactions to generate, per send period.
	Size                 int      `json:"size"`                   // The desired size of each generated transaction, in bytes.
	Count                int      `json:"count"`                  // The maximum number of transactions to send. Set to -1 for unlimited.
	BroadcastTxMethod    string   `json:"broadcast_tx_method"`    // The broadcast_tx method to use (can be "sync", "async" or "commit").
	Endpoints            []string `json:"endpoints"`              // A list of the Tendermint node endpoints to which to connect for this load test.
	EndpointSelectMethod string   `json:"endpoint_select_method"` // The method by which to select endpoints for load testing.
	ExpectPeers          int      `json:"expect_peers"`           // The minimum number of peers to expect before starting a load test. Set to 0 by default (no minimum).
	MaxEndpoints         int      `json:"max_endpoints"`          // The maximum number of endpoints to use for load testing. Set to 0 by default (no maximum).
	MinConnectivity      int      `json:"min_connectivity"`       // The minimum number of peers to which each peer must be connected before starting the load test. Set to 0 by default (no minimum).
	PeerConnectTimeout   int      `json:"peer_connect_timeout"`   // The maximum time to wait (in seconds) for all peers to connect, if ExpectPeers > 0.
	StatsOutputFile      string   `json:"stats_output_file"`      // Where to store the final aggregate statistics file (in CSV format).
	NoTrapInterrupts     bool     `json:"no_trap_interrupts"`     // Should we avoid trapping Ctrl+Break? Only relevant for standalone execution mode.
}

// CoordinatorConfig is the configuration options specific to a coordinator node.
type CoordinatorConfig struct {
	BindAddr             string `json:"bind_addr"`       // The "host:port" to which to bind the coordinator node to listen for incoming workers.
	ExpectWorkers        int    `json:"expect_workers"`  // The number of workers to expect before starting the load test.
	WorkerConnectTimeout int    `json:"connect_timeout"` // The number of seconds to wait for all workers to connect.
	ShutdownWait         int    `json:"shutdown_wait"`   // The number of seconds to wait at shutdown (while keeping the HTTP server running - primarily to allow Prometheus to keep polling).
	LoadTestID           int    `json:"load_test_id"`    // An integer greater than 0 that will be exposed via a Prometheus gauge while the load test is underway.
}

// WorkerConfig is the configuration options specific to a worker node.
type WorkerConfig struct {
	ID                  string `json:"id"`              // A unique ID for this worker instance. Will show up in the metrics reported by the coordinator for this worker.
	CoordAddr           string `json:"coord_addr"`      // The address at which to find the coordinator node.
	CoordConnectTimeout int    `json:"connect_timeout"` // The maximum amount of time, in seconds, to allow for the coordinator to become available.
}

var validBroadcastTxMethods = map[string]interface{}{
	"async":  nil,
	"sync":   nil,
	"commit": nil,
}

func (c Config) Validate() error {
	if len(c.ClientFactory) == 0 {
		return fmt.Errorf("client factory name must be specified")
	}
	factory, factoryExists := clientFactories[c.ClientFactory]
	if !factoryExists {
		return fmt.Errorf("client factory \"%s\" does not exist", c.ClientFactory)
	}
	// client factory-specific configuration validation
	if err := factory.ValidateConfig(c); err != nil {
		return fmt.Errorf("invalid configuration for client factory \"%s\": %v", c.ClientFactory, err)
	}
	if c.Connections < 1 {
		return fmt.Errorf("expected connections to be >= 1, but was %d", c.Connections)
	}
	if c.Time < 1 {
		return fmt.Errorf("expected load test time to be >= 1 second, but was %d", c.Time)
	}
	if c.SendPeriod < 1 {
		return fmt.Errorf("expected transaction send period to be >= 1 second, but was %d", c.SendPeriod)
	}
	if c.Rate < 1 {
		return fmt.Errorf("expected transaction rate to be >= 1, but was %d", c.Rate)
	}
	if c.Count < 1 && c.Count != -1 {
		return fmt.Errorf("expected max transaction count to either be -1 or >= 1, but was %d", c.Count)
	}
	if _, ok := validBroadcastTxMethods[c.BroadcastTxMethod]; !ok {
		return fmt.Errorf("expected broadcast_tx method to be one of \"sync\", \"async\" or \"commit\", but was %s", c.BroadcastTxMethod)
	}
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("expected at least one endpoint to conduct load test against, but found none")
	}
	if _, ok := validEndpointSelectMethods[c.EndpointSelectMethod]; !ok {
		return fmt.Errorf("invalid endpoint-select-method: %s", c.EndpointSelectMethod)
	}
	if c.ExpectPeers < 0 {
		return fmt.Errorf("expect-peers must be at least 0, but got %d", c.ExpectPeers)
	}
	if c.ExpectPeers > 0 && c.PeerConnectTimeout < 1 {
		return fmt.Errorf("peer-connect-timeout must be at least 1 if expect-peers is non-zero, but got %d", c.PeerConnectTimeout)
	}
	if c.MaxEndpoints < 0 {
		return fmt.Errorf("invalid value for max-endpoints: %d", c.MaxEndpoints)
	}
	if c.MinConnectivity < 0 {
		return fmt.Errorf("invalid value for min-peer-connectivity: %d", c.MinConnectivity)
	}
	return nil
}

// MaxTxsPerEndpoint estimates the maximum number of transactions that this
// configuration would generate for a single endpoint.
func (c Config) MaxTxsPerEndpoint() uint64 {
	if c.Count > -1 {
		return uint64(c.Count)
	}
	return uint64(c.Rate) * uint64(c.Time)
}

func (c CoordinatorConfig) ToJSON() string {
	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Sprintf("%v", c)
	}
	return string(b)
}

func (c CoordinatorConfig) Validate() error {
	if len(c.BindAddr) == 0 {
		return fmt.Errorf("coordinator bind address must be specified")
	}
	if c.ExpectWorkers < 1 {
		return fmt.Errorf("coordinator expect-workers must be at least 1, but got %d", c.ExpectWorkers)
	}
	if c.WorkerConnectTimeout < 1 {
		return fmt.Errorf("coordinator connect-timeout must be at least 1 second")
	}
	if c.LoadTestID < 0 {
		return fmt.Errorf("coordinator load-test-id must be 0 or greater")
	}
	return nil
}

func (c Config) ToJSON() string {
	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Sprintf("%v", c)
	}
	return string(b)
}

func (c WorkerConfig) Validate() error {
	if len(c.ID) > 0 && !isValidWorkerID(c.ID) {
		return fmt.Errorf("Invalid worker ID \"%s\": worker IDs can only be lowercase alphanumeric characters", c.ID)
	}
	if len(c.CoordAddr) == 0 {
		return fmt.Errorf("coordinator address must be specified")
	}
	if c.CoordConnectTimeout < 1 {
		return fmt.Errorf("expected connect-timeout to be >= 1, but was %d", c.CoordConnectTimeout)
	}
	return nil
}

func (c WorkerConfig) ToJSON() string {
	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Sprintf("%v", c)
	}
	return string(b)
}
