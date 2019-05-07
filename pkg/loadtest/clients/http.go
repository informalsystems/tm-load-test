package clients

import (
	"bytes"
	"fmt"
	"math/rand"
	"net/url"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/tendermint/tendermint/rpc/client"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
)

// KVStoreHTTPClientType instantiates our KVStoreHTTPFactory instance for use
// from within a slave.
type KVStoreHTTPClientType struct {
	logger logging.Logger
}

// KVStoreHTTPFactory allows us to build RPC clients for interaction with
// Tendermint nodes running the `kvstore` ABCI application (via the HTTP RPC
// endpoints).
type KVStoreHTTPFactory struct {
	cfg     Config
	id      string // A unique identifier for this factory.
	targets []string
	metrics *KVStoreHTTPCombinedMetrics // Metrics for this factory's clients.
}

// KVStoreHTTPClient is a load testing client that interacts with multiple
// different Tendermint nodes in a Tendermint network running the `kvstore` ABCI
// application (via the HTTP RPC endpoints).
type KVStoreHTTPClient struct {
	factory *KVStoreHTTPFactory // The factory to which this client belongs.
	targets []*client.HTTP      // RPC targets
}

// KVStoreHTTPMetrics helps represent either interactions' or requests'
// statistics.
type KVStoreHTTPMetrics struct {
	Count         prometheus.Counter
	Failures      prometheus.Counter
	Errors        *prometheus.CounterVec
	ResponseTimes prometheus.Summary
}

// KVStoreHTTPCombinedMetrics encapsulates the metrics relevant to the load test.
type KVStoreHTTPCombinedMetrics struct {
	Clients prometheus.Gauge // The total number of clients running right now.

	Interactions *KVStoreHTTPMetrics

	// Request-related metrics
	Requests map[string]*KVStoreHTTPMetrics
}

var _ ClientType = (*KVStoreHTTPClientType)(nil)
var _ Factory = (*KVStoreHTTPFactory)(nil)
var _ Client = (*KVStoreHTTPClient)(nil)

func newKVStoreHTTPMetrics(kind, desc, host string) *KVStoreHTTPMetrics {
	return &KVStoreHTTPMetrics{
		Count: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("loadtest_kvstorehttp_%s_%s_total", kind, host),
				Help: fmt.Sprintf("Total number of %s with the kvstore app via the HTTP RPC during load testing", desc),
			},
		),
		Failures: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("loadtest_kvstorehttp_%s_%s_failures_total", kind, host),
				Help: fmt.Sprintf("Number of %s failures with the kvstore app via the HTTP RPC during load testing", desc),
			},
		),
		Errors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("loadtest_kvstorehttp_%s_%s_errors_total", kind, host),
				Help: fmt.Sprintf("Error counts for different kinds of failures for %s", desc),
			},
			[]string{"Error"},
		),
		ResponseTimes: promauto.NewSummary(
			prometheus.SummaryOpts{
				Name:       fmt.Sprintf("loadtest_kvstorehttp_%s_%s_response_times_ms", kind, host),
				Help:       fmt.Sprintf("Response time summary (in milliseconds) for %s with the kvstore app via the HTTP RPC during load testing", desc),
				Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
			},
		),
	}
}

// ----------------------------------------------------------------------------
// KVStoreHTTPClientType
//

// NewKVStoreHTTPClientType creates a new KVStoreHTTPClientType instance ready
// to produce client factories.
func NewKVStoreHTTPClientType() *KVStoreHTTPClientType {
	return &KVStoreHTTPClientType{
		logger: logging.NewLogrusLogger(""),
	}
}

// NewFactory instantiates a KVStoreHTTPFactory with the given parameters.
func (ct *KVStoreHTTPClientType) NewFactory(cfg Config, id string) (Factory, error) {
	ct.logger.Debug("Creating Prometheus metrics", "factoryID", id)
	return &KVStoreHTTPFactory{
		cfg:     cfg,
		id:      id,
		targets: make([]string, 0),
		metrics: &KVStoreHTTPCombinedMetrics{
			Clients: promauto.NewGauge(
				prometheus.GaugeOpts{
					Name: fmt.Sprintf("loadtest_kvstorehttp_%s_clients", id),
					Help: "Total number of clients spawned",
				},
			),
			Interactions: newKVStoreHTTPMetrics("interactions", "interactions", id),
			Requests: map[string]*KVStoreHTTPMetrics{
				"broadcast_tx_sync": newKVStoreHTTPMetrics("broadcast_tx_sync", "broadcast_tx_sync requests", id),
				"abci_query":        newKVStoreHTTPMetrics("abci_query", "abci_query requests", id),
			},
		},
	}, nil
}

// ----------------------------------------------------------------------------
// KVStoreHTTPFactory
//

// SetTargets parses the given list of target strings and ensures that they're
// all HTTP/HTTPS URLs.
func (f *KVStoreHTTPFactory) SetTargets(targets []string) error {
	var httpTargets []string
	for _, target := range targets {
		u, err := url.Parse(target)
		if err != nil {
			return err
		}
		if u.Scheme == "tcp" {
			u.Scheme = "http"
		}
		httpTargets = append(httpTargets, u.String())
	}
	f.targets = httpTargets
	return nil
}

// NewClient instantiates a new client for interaction with a Tendermint
// network.
func (f *KVStoreHTTPFactory) NewClient() Client {
	return NewKVStoreHTTPClient(f)
}

// ----------------------------------------------------------------------------
// KVStoreHTTPClient
//

// NewKVStoreHTTPClient instantiates a Tendermint RPC-based load testing client.
func NewKVStoreHTTPClient(factory *KVStoreHTTPFactory) *KVStoreHTTPClient {
	targets := make([]*client.HTTP, 0)
	for _, url := range factory.targets {
		targets = append(targets, client.NewHTTP(url, "/websocket"))
	}
	return &KVStoreHTTPClient{
		factory: factory,
		targets: targets,
	}
}

func (c *KVStoreHTTPClient) randomTarget() *client.HTTP {
	return c.targets[rand.Intn(len(c.targets))]
}

func (c *KVStoreHTTPClient) measureInteraction(fn func() error) {
	timeTaken, err := TimeFn(fn)
	c.factory.metrics.Interactions.ResponseTimes.Observe(timeTaken.Seconds() * 1000)

	if err != nil {
		c.factory.metrics.Interactions.Failures.Inc()
		c.factory.metrics.Interactions.Errors.WithLabelValues(err.Error()).Inc()
	}
	// we always increment the number of interactions
	c.factory.metrics.Interactions.Count.Inc()
}

func (c *KVStoreHTTPClient) measureRequest(reqID string, fn func() error) error {
	startTime := time.Now()
	err := fn()
	timeTaken := time.Since(startTime)
	c.factory.metrics.Requests[reqID].ResponseTimes.Observe(timeTaken.Seconds() * 1000)

	if err != nil {
		c.factory.metrics.Requests[reqID].Failures.Inc()
		c.factory.metrics.Requests[reqID].Errors.WithLabelValues(err.Error()).Inc()
	}
	c.factory.metrics.Requests[reqID].Count.Inc()

	return err
}

// Interact will attempt to put a value into a Tendermint node, and then, after
// a small delay, attempt to retrieve it.
func (c *KVStoreHTTPClient) Interact() {
	c.measureInteraction(func() error {
		RandomSleep(c.factory.cfg.RequestWaitMin.Duration(), c.factory.cfg.RequestWaitMax.Duration())
		k, v, tx := MakeTxKV()
		err := c.measureRequest("broadcast_tx_sync", func() error {
			_, err := c.randomTarget().BroadcastTxSync(tx)
			return err
		})
		if err != nil {
			return err
		}

		var qres *ctypes.ResultABCIQuery
		RandomSleep(c.factory.cfg.RequestWaitMin.Duration(), c.factory.cfg.RequestWaitMax.Duration())
		err = c.measureRequest("abci_query", func() error {
			var e error
			qres, e = c.randomTarget().ABCIQuery("/key", k)
			if e != nil {
				return e
			}
			if qres.Response.IsErr() {
				return fmt.Errorf("Failed to execute ABCIQuery: %s", qres.Response.String())
			}
			if len(qres.Response.Value) == 0 {
				return fmt.Errorf("Key/value pair could not be found")
			}
			return nil
		})
		if err != nil {
			return err
		}

		if !bytes.Equal(v, qres.Response.Value) {
			return fmt.Errorf("Retrieved value does not match stored value")
		}
		return nil
	})
}

// OnStartup is called prior to the first interaction's execution.
func (c *KVStoreHTTPClient) OnStartup() error {
	c.factory.metrics.Clients.Inc()
	return nil
}

// OnShutdown is called once this client is finished interacting.
func (c *KVStoreHTTPClient) OnShutdown() {
	c.factory.metrics.Clients.Dec()
}
