package clients

import (
	"sort"
	"strings"
)

// FactoryProducer produces client factories.
type FactoryProducer interface {
	// New must take the given parameters and construct a Factory.
	New(cfg Config, host string, targets []string) Factory
}

// Factory produces clients.
type Factory interface {
	New() Client
}

// Client instances are responsible for performing interactions with Tendermint
// nodes.
type Client interface {
	// Interact is intended to perform the relevant requests that make up an
	// interaction with a Tendermint node. It doesn't return any errors because
	// the assumption is that failures should be tracked internally by a client
	// and exposed somehow through the Prometheus statistics.
	Interact()
}

var fpRegistry = make(map[string]FactoryProducer)

// RegisterFactoryProducer allows us to register a particular kind of client
// factory with a unique ID to make it available to the load testing
// infrastructure via the configuration file.
func RegisterFactoryProducer(id string, producer FactoryProducer) {
	fpRegistry[id] = producer
}

// GetFactoryProducer will attempt to look up the client factory with the given
// ID. If it exists, it will be returned. If not, nil will be returned.
func GetFactoryProducer(id string) FactoryProducer {
	producer, ok := fpRegistry[id]
	if !ok {
		return nil
	}
	return producer
}

// GetSupportedFactoryProducerIDs returns a list of supported client
// factory producer IDs.
func GetSupportedFactoryProducerIDs() []string {
	ids := make([]string, 0)
	for id := range fpRegistry {
		ids = append(ids, id)
	}
	sort.SliceStable(ids[:], func(i, j int) bool {
		return strings.Compare(ids[i], ids[j]) < 0
	})
	return ids
}
