package loadtest

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest/messages"

	cmn "github.com/tendermint/tendermint/libs/common"
	"github.com/tendermint/tendermint/rpc/client"
)

// ClientParams allows us to encapsulate the parameters for instantiating a load
// testing client.
type ClientParams struct {
	TargetNodes        []string      // The addresses of the target nodes for this client.
	InteractionTimeout time.Duration // The maximum time to allow for an interaction.
	RequestWaitMin     time.Duration // The minimum time to wait prior to executing a request.
	RequestWaitMax     time.Duration // The maximum time to wait prior to executing a request.
	RequestTimeout     time.Duration // The maximum time to allow for a request.
	TotalClients       int64         // The total number of clients for which to initialize the load testing.
}

// ClientFactory produces clients.
type ClientFactory interface {
	// NewStats must create and initialize an empty CombinedStats object for
	// this kind of client.
	NewStats(params ClientParams) *messages.CombinedStats

	NewClient(params ClientParams) Client
}

// Client instances are responsible for performing interactions with Tendermint
// nodes.
type Client interface {
	// Interact is intended to perform the relevant requests that make up an
	// interaction with a Tendermint node.
	Interact()

	// GetStats must return the combined statistics for all interactions and
	// requests for this client.
	GetStats() *messages.CombinedStats
}

var clientFactoryRegistry = make(map[string]ClientFactory)

func init() {
	RegisterClientFactory("noop", &NoopClientFactory{})
	RegisterClientFactory("tmrpc", &RPCClientFactory{})
}

// RegisterClientFactory allows us to register a particular kind of client
// factory with a unique ID to make it available to the load testing
// infrastructure via the configuration file.
func RegisterClientFactory(id string, factory ClientFactory) {
	clientFactoryRegistry[id] = factory
}

// GetClientFactory will attempt to look up the client factory with the given
// ID. If it exists, it will be returned. If not, nil will be returned.
func GetClientFactory(id string) ClientFactory {
	factory, ok := clientFactoryRegistry[id]
	if !ok {
		return nil
	}
	return factory
}

// GetSupportedClientFactoryIDs returns a list of supported client factory IDs.
func GetSupportedClientFactoryIDs() []string {
	ids := make([]string, 0)
	for id := range clientFactoryRegistry {
		ids = append(ids, id)
	}
	sort.SliceStable(ids[:], func(i, j int) bool {
		return strings.Compare(ids[i], ids[j]) < 0
	})
	return ids
}

//
// Client helpers
//

// MakeTxKV returns a text transaction, along with expected key, value pair
func MakeTxKV() ([]byte, []byte, []byte) {
	k := []byte(cmn.RandStr(8))
	v := []byte(cmn.RandStr(8))
	return k, v, append(k, append([]byte("="), v...)...)
}

// RandomSleep will sleep for a random period of between the given minimum and
// maximum times.
func RandomSleep(minSleep, maxSleep time.Duration) {
	if minSleep == maxSleep {
		time.Sleep(minSleep)
	} else {
		time.Sleep(minSleep + time.Duration(rand.Int63n(int64(maxSleep)-int64(minSleep))))
	}
}

//
// NoopClient
//

// NoopClientFactory builds noop clients (that do nothing).
type NoopClientFactory struct{}

func (f *NoopClientFactory) NewClient(params ClientParams) Client {
	return NewNoopClient(f)
}

func (f *NoopClientFactory) NewStats(_ ClientParams) *messages.CombinedStats {
	return &messages.CombinedStats{
		Interactions: NewSummaryStats(1*time.Minute, 1),
		Requests:     make(map[string]*messages.SummaryStats, 1),
	}
}

var _ ClientFactory = (*NoopClientFactory)(nil)

// NoopClient is a client that does nothing.
type NoopClient struct {
	stats *messages.CombinedStats
}

// NoopClient implements Client.
var _ Client = (*NoopClient)(nil)

func NewNoopClient(factory *NoopClientFactory) *NoopClient {
	return &NoopClient{
		stats: factory.NewStats(ClientParams{}),
	}
}

// Interact does nothing.
func (c *NoopClient) Interact() {}

// GetStats returns an empty set of statistics.
func (c *NoopClient) GetStats() *messages.CombinedStats {
	return c.stats
}

//
// RPCClient
//

// RPCClientFactory allows us to build RPC clients.
type RPCClientFactory struct{}

// RPCClient is a load testing client that interacts with multiple different
// Tendermint nodes in a Tendermint network.
type RPCClient struct {
	targets            []*client.HTTP
	interactionTimeout time.Duration
	requestWaitMin     time.Duration
	requestWaitMax     time.Duration
	requestTimeout     time.Duration
	stats              *messages.CombinedStats
}

// RPCClientFactory implements ClientFactory.
var _ ClientFactory = (*RPCClientFactory)(nil)

// RPCClient implements Client.
var _ Client = (*RPCClient)(nil)

// NewStats initializes an empty CombinedStats object for this RPC client.
func (f *RPCClientFactory) NewStats(params ClientParams) *messages.CombinedStats {
	return &messages.CombinedStats{
		Interactions: NewSummaryStats(params.InteractionTimeout, params.TotalClients),
		Requests: map[string]*messages.SummaryStats{
			"broadcast_tx_sync": NewSummaryStats(params.RequestTimeout, params.TotalClients),
			"abci_query":        NewSummaryStats(params.RequestTimeout, params.TotalClients),
		},
	}
}

// NewClient instantiates a new client for interaction with a Tendermint
// network.
func (f *RPCClientFactory) NewClient(params ClientParams) Client {
	return NewRPCClient(
		f,
		params.TargetNodes,
		params.InteractionTimeout,
		params.RequestWaitMin,
		params.RequestWaitMax,
		params.RequestTimeout,
	)
}

// NewRPCClient instantiates a Tendermint RPC-based load testing client.
func NewRPCClient(factory *RPCClientFactory, targetURLs []string, interactionTimeout, requestWaitMin, requestWaitMax, requestTimeout time.Duration) *RPCClient {
	targets := make([]*client.HTTP, 0)
	for _, url := range targetURLs {
		targets = append(targets, client.NewHTTP(url, "/websocket"))
	}
	return &RPCClient{
		targets:            targets,
		interactionTimeout: interactionTimeout,
		requestWaitMin:     requestWaitMin,
		requestWaitMax:     requestWaitMax,
		requestTimeout:     requestTimeout,
		stats: factory.NewStats(ClientParams{
			InteractionTimeout: interactionTimeout,
			RequestWaitMin:     requestWaitMin,
			RequestWaitMax:     requestWaitMax,
			RequestTimeout:     requestTimeout,
			TotalClients:       1, // This is considered to be a single client.
		}),
	}
}

func (c *RPCClient) randomTarget() *client.HTTP {
	return c.targets[rand.Intn(len(c.targets))]
}

// Interact will attempt to put a value into a Tendermint node, and then, after
// a small delay, attempt to retrieve it.
func (c *RPCClient) Interact() {
	RandomSleep(c.requestWaitMin, c.requestWaitMax)
	k, v, tx := MakeTxKV()
	startTime := time.Now()
	_, err := c.randomTarget().BroadcastTxSync(tx)
	requestTimeTaken := time.Since(startTime)
	totalTimeTaken := requestTimeTaken
	AddStatistic(c.stats.Requests["broadcast_tx_sync"], requestTimeTaken, err)
	if err != nil {
		AddStatistic(c.stats.Interactions, totalTimeTaken, err)
		return
	}

	RandomSleep(c.requestWaitMin, c.requestWaitMax)
	startTime = time.Now()
	qres, err := c.randomTarget().ABCIQuery("/key", k)
	requestTimeTaken = time.Since(startTime)
	totalTimeTaken += requestTimeTaken
	AddStatistic(c.stats.Requests["abci_query"], requestTimeTaken, err)
	if err != nil {
		AddStatistic(c.stats.Interactions, totalTimeTaken, err)
		return
	}
	if qres.Response.IsErr() {
		AddStatistic(c.stats.Interactions, totalTimeTaken, fmt.Errorf("Failed to execute ABCIQuery: %s", qres.Response.String()))
		return
	}
	if len(qres.Response.Value) == 0 {
		AddStatistic(c.stats.Interactions, totalTimeTaken, fmt.Errorf("Key/value pair could not be found"))
		return
	}
	if !bytes.Equal(v, qres.Response.Value) {
		AddStatistic(c.stats.Interactions, totalTimeTaken, fmt.Errorf("Retrieved value does not match stored value"))
		return
	}
	AddStatistic(c.stats.Interactions, totalTimeTaken, err)
}

// GetStats returns the aggregated interaction and request statistics for all of
// this client's interactions with the Tendermint nodes.
func (c *RPCClient) GetStats() *messages.CombinedStats {
	return c.stats
}
