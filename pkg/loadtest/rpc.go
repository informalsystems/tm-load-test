// This file maintains data structures that correspond to those provided by
// Tendermint Core. Given that Tendermint Core is being archived, we currently
// consider it safer to replicate these structures here than rely on the
// original repository.
//
// TODO(thane): Remove and replace with types provided by Informal Systems' fork of Tendermint Core.
package loadtest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HexBytes enables HEX-encoding for json/encoding.
type HexBytes []byte

func (bz HexBytes) MarshalJSON() ([]byte, error) {
	s := strings.ToUpper(hex.EncodeToString(bz))
	jbz := make([]byte, len(s)+2)
	jbz[0] = '"'
	copy(jbz[1:], s)
	jbz[len(jbz)-1] = '"'
	return jbz, nil
}

func (bz *HexBytes) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return fmt.Errorf("invalid hex string: %s", data)
	}
	bz2, err := hex.DecodeString(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}
	*bz = bz2
	return nil
}

// JSONStrInt is an integer whose JSON representation is a string.
type JSONStrInt int

func (i *JSONStrInt) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	val, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("failed to unmarshal int from string: %w", err)
	}
	*i = JSONStrInt(val)
	return nil
}

// JSONStrUint64 is a uint64 whose JSON representation is a string.
type JSONStrUint64 uint64

func (i *JSONStrUint64) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to unmarshal uint64 from string: %w", err)
	}
	*i = JSONStrUint64(val)
	return nil
}

// JSONStrInt64 is a int64 whose JSON representation is a string.
type JSONStrInt64 int64

func (i *JSONStrInt64) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to unmarshal int64 from string: %w", err)
	}
	*i = JSONStrInt64(val)
	return nil
}

// JSONDuration is a time.Duration whose JSON representation is a string
// containing an integer value.
type JSONDuration time.Duration

func (i *JSONDuration) UnmarshalJSON(data []byte) error {
	var ii JSONStrInt64
	if err := json.Unmarshal(data, &ii); err != nil {
		return err
	}
	*i = JSONDuration(ii)
	return nil
}

// RPCRequest corresponds to the JSON-RPC request data format accepted by
// Tendermint Core v0.34.x.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"` // must be map[string]interface{} or []interface{}
}

type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// NetInfo corresponds to the JSON-RPC response format produced by the
// Tendermint Core v0.34.x net_info RPC API.
type NetInfo struct {
	Listening bool       `json:"listening"`
	Listeners []string   `json:"listeners"`
	NPeers    JSONStrInt `json:"n_peers"`
	Peers     []Peer     `json:"peers"`
}

// Peer represents a network peer.
type Peer struct {
	NodeInfo         DefaultNodeInfo  `json:"node_info"`
	IsOutbound       bool             `json:"is_outbound"`
	ConnectionStatus ConnectionStatus `json:"connection_status"`
	RemoteIP         string           `json:"remote_ip"`
}

// DefaultNodeInfo is the basic node information exchanged
// between two peers during the Tendermint P2P handshake.
type DefaultNodeInfo struct {
	ProtocolVersion ProtocolVersion `json:"protocol_version"`

	DefaultNodeID string `json:"id"`          // authenticated identifier
	ListenAddr    string `json:"listen_addr"` // accepting incoming

	Network  string   `json:"network"`  // network/chain ID
	Version  string   `json:"version"`  // major.minor.revision
	Channels HexBytes `json:"channels"` // channels this node knows about

	Moniker string               `json:"moniker"` // arbitrary moniker
	Other   DefaultNodeInfoOther `json:"other"`   // other application specific data
}

// ProtocolVersion contains the protocol versions for the software.
type ProtocolVersion struct {
	P2P   JSONStrUint64 `json:"p2p"`
	Block JSONStrUint64 `json:"block"`
	App   JSONStrUint64 `json:"app"`
}

// DefaultNodeInfoOther is the misc. applcation specific data
type DefaultNodeInfoOther struct {
	TxIndex    string `json:"tx_index"`
	RPCAddress string `json:"rpc_address"`
}

type ConnectionStatus struct {
	Duration    JSONDuration
	SendMonitor FlowStatus
	RecvMonitor FlowStatus
	Channels    []ChannelStatus
}

// FlowStatus represents the current Monitor status. All transfer rates are in bytes
// per second rounded to the nearest byte.
type FlowStatus struct {
	Start    time.Time    // Transfer start time
	Bytes    JSONStrInt64 // Total number of bytes transferred
	Samples  JSONStrInt64 // Total number of samples taken
	InstRate JSONStrInt64 // Instantaneous transfer rate
	CurRate  JSONStrInt64 // Current transfer rate (EMA of InstRate)
	AvgRate  JSONStrInt64 // Average transfer rate (Bytes / Duration)
	PeakRate JSONStrInt64 // Maximum instantaneous transfer rate
	BytesRem JSONStrInt64 // Number of bytes remaining in the transfer
	Duration JSONDuration // Time period covered by the statistics
	Idle     JSONDuration // Time since the last transfer of at least 1 byte
	TimeRem  JSONDuration // Estimated time to completion
	Progress Percent      // Overall transfer progress
	Active   bool         // Flag indicating an active transfer
}

type ChannelStatus struct {
	ID                byte
	SendQueueCapacity JSONStrInt
	SendQueueSize     JSONStrInt
	Priority          JSONStrInt
	RecentlySent      JSONStrInt64
}

// Percent represents a percentage in increments of 1/1000th of a percent.
type Percent uint32

type httpClient struct {
	addr   string
	client *http.Client
}

// Returns an HTTP client configuration.
func newHttpRpcClient(addr string) *httpClient {
	addr = strings.TrimRight(addr, "/")
	return &httpClient{
		addr: addr,
		client: &http.Client{
			Transport: &http.Transport{
				// Prevent zip bombs
				DisableCompression: true,
			},
		},
	}
}

func (c *httpClient) netInfo() (*NetInfo, error) {
	httpRes, err := c.client.Get(c.addr + "/net_info")
	if err != nil {
		return nil, fmt.Errorf("failed to get net_info for peer %s: %w", c.addr, err)
	}
	defer httpRes.Body.Close()

	resBytes, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return nil, err
	}

	res := &RPCResponse{}
	if err := json.Unmarshal(resBytes, res); err != nil {
		return nil, fmt.Errorf("failed to unmarshal net_info response for peer %s: %w", c.addr, err)
	}
	if res.Error != nil && res.Error.Code != 0 {
		return nil, fmt.Errorf("got error code %d when attempting to get net_info for %s: %s", res.Error.Code, c.addr, res.Error.Message)
	}
	netInfo := &NetInfo{}
	if err := json.Unmarshal(res.Result, netInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal net_info inner response for peer %s: %w", c.addr, err)
	}
	return netInfo, nil
}
