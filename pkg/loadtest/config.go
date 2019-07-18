package loadtest

import (
	"encoding/json"
	"fmt"
)

// Config represents the configuration for a single client (i.e. standalone or
// slave).
type Config struct {
	ClientFactory     string   `json:"client_factory"`      // Which client factory should we use for load testing?
	Connections       int      `json:"connections"`         // The number of WebSockets connections to make to each target endpoint.
	Time              int      `json:"time"`                // The total time, in seconds, for which to run the load test.
	SendPeriod        int      `json:"send_period"`         // The period (in seconds) at which to send batches of transactions.
	Rate              int      `json:"rate"`                // The number of transactions to generate, per send period.
	Size              int      `json:"size"`                // The desired size of each generated transaction, in bytes.
	Count             int      `json:"count"`               // The maximum number of transactions to send. Set to -1 for unlimited.
	BroadcastTxMethod string   `json:"broadcast_tx_method"` // The broadcast_tx method to use (can be "sync", "async" or "commit").
	Endpoints         []string `json:"endpoints"`           // A list of the Tendermint node endpoints to which to connect for this load test.
}

// MasterConfig is the configuration options specific to a master node.
type MasterConfig struct {
	BindAddr            string `json:"bind_addr"`       // The "host:port" to which to bind the master node to listen for incoming slaves.
	ExpectSlaves        int    `json:"expect_slaves"`   // The number of slaves to expect before starting the load test.
	SlaveConnectTimeout int    `json:"connect_timeout"` // The number of seconds to wait for all slaves to connect.
}

// SlaveConfig is the configuration options specific to a slave node.
type SlaveConfig struct {
	MasterAddr           string `json:"master_addr"`     // The address at which to find the master node.
	MasterConnectTimeout int    `json:"connect_timeout"` // The maximum amount of time, in seconds, to allow for the master to become available.
}

var validBroadcastTxMethods = map[string]interface{}{
	"async":  nil,
	"sync":   nil,
	"commit": nil,
}

func (c Config) Validate() error {
	if len(c.ClientFactory) == 0 {
		return fmt.Errorf("Client factory name must be specified")
	}
	if _, exists := clientFactories[c.ClientFactory]; !exists {
		return fmt.Errorf("Client factory \"%s\" does not exist", c.ClientFactory)
	}
	if c.Connections < 1 {
		return fmt.Errorf("Expected connections to be >= 1, but was %d", c.Connections)
	}
	if c.Time < 1 {
		return fmt.Errorf("Expected load test time to be >= 1 second, but was %d", c.Time)
	}
	if c.SendPeriod < 1 {
		return fmt.Errorf("Expected transaction send period to be >= 1 second, but was %d", c.SendPeriod)
	}
	if c.Rate < 1 {
		return fmt.Errorf("Expected transaction rate to be >= 1, but was %d", c.Rate)
	}
	if c.Size < 40 {
		return fmt.Errorf("Expected transaction size to be >= 40 bytes, but was %d", c.Size)
	}
	if c.Count < 1 && c.Count != -1 {
		return fmt.Errorf("Expected max transaction count to either be -1 or >= 1, but was %d", c.Count)
	}
	if _, ok := validBroadcastTxMethods[c.BroadcastTxMethod]; !ok {
		return fmt.Errorf("Expected broadcast_tx method to be one of \"sync\", \"async\" or \"commit\", but was %s", c.BroadcastTxMethod)
	}
	if len(c.Endpoints) == 0 {
		return fmt.Errorf("Expected at least one endpoint to conduct load test against, but found none")
	}
	return nil
}

func (c MasterConfig) ToJSON() string {
	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Sprintf("%v", c)
	}
	return string(b)
}

func (c MasterConfig) Validate() error {
	if len(c.BindAddr) == 0 {
		return fmt.Errorf("Master bind address must be specified")
	}
	if c.ExpectSlaves < 1 {
		return fmt.Errorf("Master expect-slaves must be at least 1, but got %d", c.ExpectSlaves)
	}
	if c.SlaveConnectTimeout < 1 {
		return fmt.Errorf("Master connect-timeout must be at least 1 second")
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

func (c SlaveConfig) Validate() error {
	if len(c.MasterAddr) == 0 {
		return fmt.Errorf("Master address must be specified")
	}
	if c.MasterConnectTimeout < 1 {
		return fmt.Errorf("Expected connect-timeout to be >= 1, but was %d", c.MasterConnectTimeout)
	}
	return nil
}

func (c SlaveConfig) ToJSON() string {
	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Sprintf("%v", c)
	}
	return string(b)
}
