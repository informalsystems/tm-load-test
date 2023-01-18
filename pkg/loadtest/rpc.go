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
	Listening bool     `json:"listening"`
	Listeners []string `json:"listeners"`
	NPeers    int      `json:"n_peers"`
	Peers     []Peer   `json:"peers"`
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
	P2P   uint64 `json:"p2p"`
	Block uint64 `json:"block"`
	App   uint64 `json:"app"`
}

// DefaultNodeInfoOther is the misc. applcation specific data
type DefaultNodeInfoOther struct {
	TxIndex    string `json:"tx_index"`
	RPCAddress string `json:"rpc_address"`
}

type ConnectionStatus struct {
	Duration    time.Duration
	SendMonitor FlowStatus
	RecvMonitor FlowStatus
	Channels    []ChannelStatus
}

// FlowStatus represents the current Monitor status. All transfer rates are in bytes
// per second rounded to the nearest byte.
type FlowStatus struct {
	Start    time.Time     // Transfer start time
	Bytes    int64         // Total number of bytes transferred
	Samples  int64         // Total number of samples taken
	InstRate int64         // Instantaneous transfer rate
	CurRate  int64         // Current transfer rate (EMA of InstRate)
	AvgRate  int64         // Average transfer rate (Bytes / Duration)
	PeakRate int64         // Maximum instantaneous transfer rate
	BytesRem int64         // Number of bytes remaining in the transfer
	Duration time.Duration // Time period covered by the statistics
	Idle     time.Duration // Time since the last transfer of at least 1 byte
	TimeRem  time.Duration // Estimated time to completion
	Progress Percent       // Overall transfer progress
	Active   bool          // Flag indicating an active transfer
}

type ChannelStatus struct {
	ID                byte
	SendQueueCapacity int
	SendQueueSize     int
	Priority          int
	RecentlySent      int64
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
	return nil, nil
}
