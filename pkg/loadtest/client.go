package loadtest

import "fmt"

// ClientFactory produces load testing clients.
type ClientFactory interface {
	// ValidateConfig must check whether the given configuration is valid for
	// our specific client factory.
	ValidateConfig(cfg Config) error

	// NewClient must instantiate a new load testing client, or produce an error
	// if that process fails.
	NewClient(cfg Config) (Client, error)
}

// Client generates transactions to be sent to a specific endpoint.
type Client interface {
	// GenerateTx must generate a raw transaction to be sent to the relevant
	// broadcast_tx method for a given endpoint.
	GenerateTx() ([]byte, error)
}

// Our global registry of client factories
var clientFactories = map[string]ClientFactory{}

// RegisterClientFactory allows us to programmatically register different client
// factories to easily switch between different ones at runtime.
func RegisterClientFactory(name string, factory ClientFactory) error {
	if _, exists := clientFactories[name]; exists {
		return fmt.Errorf("client factory with the specified name already exists: %s", name)
	}
	clientFactories[name] = factory
	return nil
}
