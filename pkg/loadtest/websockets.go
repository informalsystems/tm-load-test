package loadtest

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultWSReadTimeout  = 10 * time.Second
	defaultWSWriteTimeout = 10 * time.Second
	defaultWSPongWait     = 30 * time.Second
	defaultWSPingPeriod   = (defaultWSPongWait * 9) / 10
)

// simpleSocket provides a simpler interface to interact with a websockets
// connection than that provided by Gorilla. All reads and writes happen within
// a single goroutine.
type simpleSocket struct {
	conn *websocket.Conn

	flushOnStop      bool
	sendCloseMessage bool

	inbound  chan websocketReadRequest
	outbound chan websocketWriteRequest
	stop     chan struct{} // Close this to stop the primary event loop.
	stopped  chan struct{} // Closed once the event loop terminates.
}

type websocketReadRequest struct {
	timeout time.Duration
	resp    chan websocketReadResponse
}

type websocketReadResponse struct {
	mt   int
	data []byte
	err  error
}

type websocketWriteRequest struct {
	data    []byte
	timeout time.Duration
	resp    chan error
}

type simpleSocketConfig struct {
	inboundBufSize   int  // The maximum size of the inbound message buffer.
	outboundBufSize  int  // The maximum size of the outbound message buffer.
	flushOnStop      bool // Should we try to flush any remaining outbound messages when the Run operation is stopped?
	sendCloseMessage bool // Should we send a close message when the Run operation is stopped?
}

func defaultSimpleSocketConfig() *simpleSocketConfig {
	return &simpleSocketConfig{
		inboundBufSize:   10,
		outboundBufSize:  10,
		flushOnStop:      true,
		sendCloseMessage: true,
	}
}

type simpleSocketOpt func(cfg *simpleSocketConfig)

func ssInboundBufSize(size int) simpleSocketOpt {
	return func(cfg *simpleSocketConfig) {
		cfg.inboundBufSize = size
	}
}

func ssOutboundBufSize(size int) simpleSocketOpt {
	return func(cfg *simpleSocketConfig) {
		cfg.outboundBufSize = size
	}
}

func ssFlushOnStop(val bool) simpleSocketOpt {
	return func(cfg *simpleSocketConfig) {
		cfg.flushOnStop = val
	}
}

func ssSendCloseMessage(val bool) simpleSocketOpt {
	return func(cfg *simpleSocketConfig) {
		cfg.sendCloseMessage = val
	}
}

func newSimpleSocket(conn *websocket.Conn, opts ...simpleSocketOpt) *simpleSocket {
	cfg := defaultSimpleSocketConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return &simpleSocket{
		conn:             conn,
		inbound:          make(chan websocketReadRequest, cfg.inboundBufSize),
		outbound:         make(chan websocketWriteRequest, cfg.outboundBufSize),
		stop:             make(chan struct{}, 1),
		stopped:          make(chan struct{}, 1),
		flushOnStop:      cfg.flushOnStop,
		sendCloseMessage: cfg.sendCloseMessage,
	}
}

// Run is a blocking operation, and executes all of the socket's read/write
// routines and event handling in the calling goroutine. It runs forever until
// its Stop routine is called from a separate goroutine.
func (s *simpleSocket) Run() {
	defer func() {
		s.close()
		close(s.stopped)
	}()

	pingTicker := time.NewTicker(defaultWSPingPeriod)
	defer pingTicker.Stop()

	s.conn.SetPongHandler(func(appData string) error { _ = s.conn.SetReadDeadline(time.Now().Add(defaultWSPongWait)); return nil })

	for {
		select {
		case req := <-s.inbound:
			s.handleRead(req)

		case req := <-s.outbound:
			s.handleWrite(req)

		case <-pingTicker.C:
			_ = s.conn.WriteMessage(websocket.PingMessage, nil)

		case <-s.stop:
			return
		}
	}
}

// Read is a synchronous, blocking operation.
func (s *simpleSocket) Read(timeouts ...time.Duration) (int, []byte, error) {
	timeout := defaultWSReadTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}
	req := websocketReadRequest{
		timeout: timeout,
		resp:    make(chan websocketReadResponse, 1),
	}
	s.inbound <- req
	resp := <-req.resp
	return resp.mt, resp.data, resp.err
}

func (s *simpleSocket) ReadSlaveMsg(timeouts ...time.Duration) (slaveMsg, error) {
	var msg slaveMsg
	mt, data, err := s.Read(timeouts...)
	if err != nil {
		return msg, err
	}
	if mt != websocket.TextMessage {
		return msg, fmt.Errorf("expected text message (%d), but got message type %d", websocket.TextMessage, mt)
	}
	err = json.Unmarshal(data, &msg)
	return msg, err
}

// Write is a synchronous, blocking operation.
func (s *simpleSocket) Write(data []byte, timeouts ...time.Duration) error {
	timeout := defaultWSWriteTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}
	req := websocketWriteRequest{
		data:    data,
		timeout: timeout,
		resp:    make(chan error, 1),
	}
	s.outbound <- req
	return <-req.resp
}

func (s *simpleSocket) WriteSlaveMsg(msg slaveMsg, timeouts ...time.Duration) error {
	data, err := json.Marshal(&msg)
	if err != nil {
		return err
	}
	return s.Write(data, timeouts...)
}

// Stop will end Run's event loop and block until it has completely stopped.
func (s *simpleSocket) Stop() {
	close(s.stop)
	<-s.stopped
}

func (s *simpleSocket) handleRead(req websocketReadRequest) {
	deadline := time.Now().Add(req.timeout)
	_ = s.conn.SetReadDeadline(deadline)
	mt, data, err := s.conn.ReadMessage()
	req.resp <- websocketReadResponse{
		mt:   mt,
		data: data,
		err:  err,
	}
}

func (s *simpleSocket) handleWrite(req websocketWriteRequest) {
	deadline := time.Now().Add(req.timeout)
	_ = s.conn.SetWriteDeadline(deadline)
	req.resp <- s.conn.WriteMessage(websocket.TextMessage, req.data)
}

func (s *simpleSocket) close() {
	if s.flushOnStop {
		remaining := len(s.outbound)
		for i := 0; i < remaining; i++ {
			s.handleWrite(<-s.outbound)
		}
	}
	if s.sendCloseMessage {
		_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}
}
