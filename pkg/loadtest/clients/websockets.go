package clients

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	cmn "github.com/tendermint/tendermint/libs/common"
	ctypes "github.com/tendermint/tendermint/rpc/core/types"
	rpctypes "github.com/tendermint/tendermint/rpc/lib/types"
)

// KVStoreWebSocketsClientType allows us to spawn WebSockets-based clients that
// interact with the `kvstore` ABCI application.
type KVStoreWebSocketsClientType struct{}

// KVStoreWebSocketsFactory produces KVStoreWebSocketsClient instances.
type KVStoreWebSocketsFactory struct {
	logger logging.Logger

	cfg     Config
	targets []string
	id      string
	metrics *KVStoreWebSocketsCombinedMetrics
}

// KVStoreWebSocketsClient implements a client that interacts with a Tendermint
// node via the WebSockets interface.
type KVStoreWebSocketsClient struct {
	factory *KVStoreWebSocketsFactory
	target  string
	conn    *websocket.Conn // We only keep a single (long-lived) connection to a particular Tendermint node (randomly selected).
	id      string          // A unique identifier for this client.

	mtx      sync.Mutex
	flagStop bool

	recvCtrlc chan bool
	recvc     chan interface{}
	sendc     chan interface{}

	sendAndRecvLoopStopped chan struct{}
}

// KVStoreWebSocketsMetrics keeps track of interaction- or request-related
// metrics when interacting with a Tendermint node.
type KVStoreWebSocketsMetrics struct {
	Count         prometheus.Counter
	Failures      prometheus.Counter
	Errors        *prometheus.CounterVec
	ResponseTimes prometheus.Summary
}

type eventWrapper struct {
	Type  string `json:"type"`
	Value struct {
		TxResult struct {
			Height string          `json:"height"`
			Index  uint32          `json:"index"`
			Tx     string          `json:"tx"`
			Result json.RawMessage `json:"result"`
		} `json:"TxResult"`
	} `json:"value"`
}

// KVStoreWebSocketsCombinedMetrics encapsulates the metrics relevant to the load test.
type KVStoreWebSocketsCombinedMetrics struct {
	Clients      prometheus.Gauge // The total number of clients running right now.
	Interactions *KVStoreWebSocketsMetrics
}

var _ ClientType = (*KVStoreWebSocketsClientType)(nil)
var _ Factory = (*KVStoreWebSocketsFactory)(nil)
var _ Client = (*KVStoreWebSocketsClient)(nil)

func newKVStoreWebSocketsMetrics(kind, desc, host string) *KVStoreWebSocketsMetrics {
	return &KVStoreWebSocketsMetrics{
		Count: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("loadtest_kvstorewebsockets_%s_%s_total", kind, host),
				Help: fmt.Sprintf("Total number of %s with the kvstore app via the WebSockets RPC during load testing", desc),
			},
		),
		Failures: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("loadtest_kvstorewebsockets_%s_%s_failures_total", kind, host),
				Help: fmt.Sprintf("Number of %s failures with the kvstore app via the WebSockets RPC during load testing", desc),
			},
		),
		Errors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("loadtest_kvstorewebsockets_%s_%s_errors_total", kind, host),
				Help: fmt.Sprintf("Error counts for different kinds of failures for %s", desc),
			},
			[]string{"Error"},
		),
		ResponseTimes: promauto.NewSummary(
			prometheus.SummaryOpts{
				Name:       fmt.Sprintf("loadtest_kvstorewebsockets_%s_%s_response_times_ms", kind, host),
				Help:       fmt.Sprintf("Response time summary (in milliseconds) for %s with the kvstore app via the WebSockets RPC during load testing", desc),
				Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
			},
		),
	}
}

//
// KVStoreWebSocketsClientType
//

// NewKVStoreWebSocketsClientType instantiates our WebSockets-based client type.
func NewKVStoreWebSocketsClientType() *KVStoreWebSocketsClientType {
	return &KVStoreWebSocketsClientType{}
}

// NewFactory creates a new KVStoreWebSocketsFactory with the given parameters.
func (t *KVStoreWebSocketsClientType) NewFactory(cfg Config, id string) (Factory, error) {
	return &KVStoreWebSocketsFactory{
		logger:  logging.NewLogrusLogger("client-factory"),
		cfg:     cfg,
		targets: make([]string, 0),
		id:      id,
		metrics: &KVStoreWebSocketsCombinedMetrics{
			Clients: promauto.NewGauge(
				prometheus.GaugeOpts{
					Name: fmt.Sprintf("loadtest_kvstorewebsockets_%s_clients", id),
					Help: "Total number of clients spawned",
				},
			),
			Interactions: newKVStoreWebSocketsMetrics("interactions", "interactions", id),
		},
	}, nil
}

//
// KVStoreWebSocketsFactory
//

// SetTargets transforms the given list of target URLs into ws:// or wss://
// targets to ensure readiness for WebSockets-based testing.
func (f *KVStoreWebSocketsFactory) SetTargets(targets []string) error {
	// transform the targets to make sure they're WebSockets URLs
	var wsTargets []string
	for _, target := range targets {
		u, err := url.Parse(target)
		if err != nil {
			return err
		}
		switch u.Scheme {
		case "http", "tcp":
			u.Scheme = "ws"

		case "https":
			u.Scheme = "wss"
		}
		if len(u.Path) == 0 || u.Path == "/" {
			u.Path = "/websocket"
		}
		wsTargets = append(wsTargets, u.String())
	}
	f.targets = wsTargets
	return nil
}

// NewClient instantiates a single WebSockets client to interact with a randomly
// chosen Tendermint target node.
func (f *KVStoreWebSocketsFactory) NewClient() Client {
	return &KVStoreWebSocketsClient{
		factory:                f,
		target:                 f.targets[rand.Int()%len(f.targets)],
		id:                     "loadtest-" + cmn.RandStr(8),
		recvCtrlc:              make(chan bool, 1),
		sendc:                  make(chan interface{}, 1),
		recvc:                  make(chan interface{}, 1),
		sendAndRecvLoopStopped: make(chan struct{}),
	}
}

func (f *KVStoreWebSocketsFactory) randomTarget() string {
	return f.targets[rand.Int()%len(f.targets)]
}

//
// KVStoreWebSocketsClient
//

// OnStartup connects our client to a random WebSockets target.
func (c *KVStoreWebSocketsClient) OnStartup() error {
	var err error
	c.conn, _, err = websocket.DefaultDialer.Dial(c.factory.randomTarget(), nil)
	if err != nil {
		return err
	}
	c.conn.SetPingHandler(func(message string) error {
		err := c.conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(c.factory.cfg.RequestTimeout.Duration()))
		if err == websocket.ErrCloseSent {
			return nil
		} else if e, ok := err.(net.Error); ok && e.Temporary() {
			return nil
		}
		return err
	})
	go c.sendAndRecvLoop()
	c.factory.metrics.Clients.Inc()

	if err = c.subscribeToTxs(); err != nil {
		c.shutdown()
		return fmt.Errorf("failed to subscribe to Tx notifications: %v", err)
	}

	return nil
}

func (c *KVStoreWebSocketsClient) subscribeToTxs() error {
	// we want to know when blocks get created with this client's transactions in them
	query := fmt.Sprintf("tm.event='Tx' AND app.key='%s'", c.id)
	//query := "tm.event='Tx'"
	if err := c.send("subscribe", map[string]interface{}{"query": query}); err != nil {
		return err
	}
	// get the response
	_, err := c.recvSync()
	return err
}

func (c *KVStoreWebSocketsClient) sendAndRecvLoop() {
	defer close(c.sendAndRecvLoopStopped)
	stopCheckTicker := time.NewTicker(100 * time.Millisecond)
	defer stopCheckTicker.Stop()
loop:
	for {
		select {
		case <-c.recvCtrlc:
			c.conn.SetReadDeadline(time.Now().Add(c.factory.cfg.RequestTimeout.Duration())) // nolint: errcheck
			mt, p, err := c.conn.ReadMessage()
			if err != nil {
				return
			}
			if mt != websocket.TextMessage {
				continue
			}
			var res rpctypes.RPCResponse
			if err := json.Unmarshal(p, &res); err != nil {
				continue
			}
			c.recvc <- res

		case msg := <-c.sendc:
			c.conn.SetWriteDeadline(time.Now().Add(c.factory.cfg.RequestTimeout.Duration())) // nolint: errcheck
			if err := c.conn.WriteJSON(msg); err != nil {
				// TODO: add a failure handler channel here to send errors back
				// to the caller
				continue loop
			}
			// we're anticipating a response, so trigger a WebSockets read
			c.recvCtrlc <- true

		case <-stopCheckTicker.C:
			if c.mustStop() {
				return
			}
		}
	}
}

func (c *KVStoreWebSocketsClient) stop() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.flagStop = true
}

func (c *KVStoreWebSocketsClient) mustStop() bool {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.flagStop
}

func (c *KVStoreWebSocketsClient) waitUntilStopped() {
	<-c.sendAndRecvLoopStopped
}

// Interact attempts to send a value to the kvstore to be stored, and then
// attempts to read it back.
func (c *KVStoreWebSocketsClient) Interact() {
	c.measureInteraction(func() error {
		RandomSleep(c.factory.cfg.RequestWaitMin.Duration(), c.factory.cfg.RequestWaitMax.Duration())
		// we use our client ID as the key because the kvstore proxy app tags
		// transactions according to the key used - this allows us to receive
		// events for our particular transactions
		return c.putAndWaitForCommit(c.id, cmn.RandStr(8))
	})
}

func (c *KVStoreWebSocketsClient) measureInteraction(fn func() error) {
	timeTaken, err := TimeFn(fn)
	c.factory.metrics.Interactions.ResponseTimes.Observe(timeTaken.Seconds() * 1000)

	if err != nil {
		c.factory.metrics.Interactions.Failures.Inc()
		c.factory.metrics.Interactions.Errors.WithLabelValues(err.Error()).Inc()
	}
	// we always increment the number of interactions
	c.factory.metrics.Interactions.Count.Inc()
}

func (c *KVStoreWebSocketsClient) putAndWaitForCommit(k, v string) error {
	tx := []byte(k + "=" + v)
	if err := c.send("broadcast_tx_sync", map[string]interface{}{"tx": tx}); err != nil {
		return err
	}
	// get the broadcast_tx_sync response
	_, err := c.recvSync()
	if err != nil {
		return err
	}

	// now try get the subscription event too
	res, err := c.recvEvent()
	if err != nil {
		return err
	}
	// extract the transaction details
	var ev ctypes.ResultEvent
	if err := json.Unmarshal(res.Result, &ev); err != nil {
		return fmt.Errorf("subscription event result is not of correct type")
	}
	jsonData, err := json.Marshal(ev.Data)
	if err != nil {
		return fmt.Errorf("cannot convert event data to JSON: %v", err)
	}
	var wrapper eventWrapper
	if err := json.Unmarshal(jsonData, &wrapper); err != nil {
		return fmt.Errorf("failed to unmarshal event data wrapper: %v", err)
	}
	// decode the transaction
	decodedTx, err := base64.StdEncoding.DecodeString(string(wrapper.Value.TxResult.Tx))
	if err != nil {
		return err
	}
	if !bytes.Equal(tx, decodedTx) {
		return fmt.Errorf("sent transaction does not match received transaction")
	}
	return nil
}

func (c *KVStoreWebSocketsClient) send(method string, params map[string]interface{}) error {
	var p []byte
	var err error

	if params != nil {
		p, err = json.Marshal(params)
		if err != nil {
			return err
		}
	}

	c.sendc <- rpctypes.RPCRequest{
		JSONRPC: "2.0",
		ID:      rpctypes.JSONRPCStringID(c.id),
		Method:  method,
		Params:  json.RawMessage(p),
	}
	return nil
}

func (c *KVStoreWebSocketsClient) recvSync() (*rpctypes.RPCResponse, error) {
	select {
	case res := <-c.recvc:
		switch r := res.(type) {
		case rpctypes.RPCResponse:
			if r.Error != nil {
				return nil, fmt.Errorf("RPC request failed: %v", r.Error.Error())
			}
			return &r, nil

		default:
			return nil, fmt.Errorf("unrecognized response type: %v", r)
		}

	case <-time.After(c.factory.cfg.RequestTimeout.Duration()):
		return nil, fmt.Errorf("recv timed out")
	}
}

func (c *KVStoreWebSocketsClient) recvEvent() (*rpctypes.RPCResponse, error) {
	c.recvCtrlc <- true
	return c.recvSync()
}

func (c *KVStoreWebSocketsClient) unsubscribeFromTxs() error {
	if err := c.send("unsubscribe_all", nil); err != nil {
		return err
	}
	_, err := c.recvSync()
	return err
}

func (c *KVStoreWebSocketsClient) shutdown() {
	c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")) // nolint: errcheck
	// trigger a final read from the WebSockets endpoint
	c.recvCtrlc <- true
	// wait for the server to close the connection
	time.Sleep(time.Second)
	c.conn.Close() // nolint: errcheck

	c.stop()
	c.waitUntilStopped()
	c.factory.metrics.Clients.Dec()
}

// OnShutdown attempts to cleanly close the WebSockets connection.
func (c *KVStoreWebSocketsClient) OnShutdown() {
	c.unsubscribeFromTxs() // nolint: errcheck
	c.shutdown()
}
