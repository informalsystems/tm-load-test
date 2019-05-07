package clients

import (
	"sort"
	"strings"
)

// ClientType produces client factories.
type ClientType interface {
	// NewFactory must take the given parameters and construct a Factory.
	NewFactory(cfg Config, id string) (Factory, error)
}

// Factory produces clients.
type Factory interface {
	// SetTargets must allow the caller to configure the targets for load
	// testing prior to execution.
	SetTargets(targets []string) error

	// NewClient instantiates a client.
	NewClient() Client
}

// Client instances are responsible for performing interactions with Tendermint
// nodes.
type Client interface {
	// Interact is intended to perform the relevant requests that make up an
	// interaction with a Tendermint node. It doesn't return any errors because
	// the assumption is that failures should be tracked internally by a client
	// and exposed somehow through the Prometheus statistics.
	Interact()

	// OnStartup is an event that is called prior to the first interaction for
	// this client. If this function returns an error, it will cause the slave
	// to fail and thus fail the entire load testing operation.
	OnStartup() error

	// OnShutdown is called once this client has finished all of its
	// interactions.
	OnShutdown()
}

var clientTypeRegistry = make(map[string]ClientType)

// RegisterClientType allows us to register a particular kind of client factory
// with a unique ID to make it available to the load testing infrastructure via
// the configuration file. Not thread-safe.
func RegisterClientType(id string, ct ClientType) {
	clientTypeRegistry[id] = ct
}

// GetClientType will attempt to look up the client factory with the given ID.
// If it exists, it will be returned. If not, nil will be returned. Not
// thread-safe.
func GetClientType(id string) ClientType {
	ct, ok := clientTypeRegistry[id]
	if !ok {
		return nil
	}
	return ct
}

// GetSupportedClientTypes returns a list of supported client types.
func GetSupportedClientTypes() []string {
	ids := make([]string, 0)
	for id := range clientTypeRegistry {
		ids = append(ids, id)
	}
	sort.SliceStable(ids[:], func(i, j int) bool {
		return strings.Compare(ids[i], ids[j]) < 0
	})
	return ids
}
