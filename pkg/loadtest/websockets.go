package loadtest

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
	"github.com/informalsystems/tm-load-test/internal/logging"
)

const (
	defaultWSReadTimeout  = 3 * time.Second
	defaultWSWriteTimeout = 3 * time.Second
)

// simpleSocket provides a simpler interface to interact with a websockets
// connection than that provided by Gorilla. All reads and writes happen within
// two goroutines: one for reading, and one for writing.
type simpleSocket struct {
	conn   *websocket.Conn
	logger logging.Logger

	flushOnStop            bool
	sendCloseMessage       bool
	waitForRemoteClose     bool
	remoteCloseWaitTimeout time.Duration

	inbound         chan websocketReadRequest
	outbound        chan websocketWriteRequest
	stop            chan struct{} // Close this to stop the primary event loop.
	stopped         chan struct{} // Closed once the event loop terminates.
	stopReadPump    chan struct{}
	readPumpStopped chan struct{}
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
	inboundBufSize         int           // The maximum size of the inbound message buffer.
	outboundBufSize        int           // The maximum size of the outbound message buffer.
	flushOnStop            bool          // Should we try to flush any remaining outbound messages when the Run operation is stopped?
	sendCloseMessage       bool          // Should we send a close message when the Run operation is stopped?
	waitForRemoteClose     bool          // Should we wait for the remote end to close the connection?
	remoteCloseWaitTimeout time.Duration // If waitForRemoteClose is true, how long should we wait?
	parentCtx              string        // The context of the parent object responsible for managing this socket (only for logging purposes).
}

func defaultSimpleSocketConfig() *simpleSocketConfig {
	return &simpleSocketConfig{
		inboundBufSize:         10,
		outboundBufSize:        10,
		flushOnStop:            true,
		sendCloseMessage:       true,
		waitForRemoteClose:     false,
		remoteCloseWaitTimeout: 60 * time.Second,
		parentCtx:              "",
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

func ssWaitForRemoteClose(val bool) simpleSocketOpt {
	return func(cfg *simpleSocketConfig) {
		cfg.waitForRemoteClose = val
	}
}

func ssRemoteCloseWaitTimeout(timeout time.Duration) simpleSocketOpt {
	return func(cfg *simpleSocketConfig) {
		cfg.remoteCloseWaitTimeout = timeout
	}
}

func ssParentCtx(ctx string) simpleSocketOpt {
	return func(cfg *simpleSocketConfig) {
		cfg.parentCtx = ctx
	}
}

func newSimpleSocket(conn *websocket.Conn, opts ...simpleSocketOpt) *simpleSocket {
	cfg := defaultSimpleSocketConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	ctx := "simplesocket"
	if len(cfg.parentCtx) > 0 {
		ctx = fmt.Sprintf("%s.%s", cfg.parentCtx, ctx)
	}
	return &simpleSocket{
		conn:                   conn,
		logger:                 logging.NewLogrusLogger(ctx),
		inbound:                make(chan websocketReadRequest, cfg.inboundBufSize),
		outbound:               make(chan websocketWriteRequest, cfg.outboundBufSize),
		stop:                   make(chan struct{}, 1),
		stopped:                make(chan struct{}, 1),
		stopReadPump:           make(chan struct{}, 1),
		readPumpStopped:        make(chan struct{}, 1),
		flushOnStop:            cfg.flushOnStop,
		sendCloseMessage:       cfg.sendCloseMessage,
		waitForRemoteClose:     cfg.waitForRemoteClose,
		remoteCloseWaitTimeout: cfg.remoteCloseWaitTimeout,
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

	go s.readPump()
	defer func() {
		close(s.stopReadPump)
		<-s.readPumpStopped
	}()

	for {
		select {
		case req := <-s.outbound:
			s.logger.Debug("Attempting to write to WebSocket", "data", string(req.data), "timeout", req.timeout.String())
			s.handleWrite(req)

		case <-s.stop:
			s.logger.Debug("Got cancellation notification")
			return
		}
	}
}

func (s *simpleSocket) readPump() {
	defer close(s.readPumpStopped)

	for {
		select {
		case req := <-s.inbound:
			s.handleRead(req)

		case <-s.stopReadPump:
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
	select {
	case resp := <-req.resp:
		return resp.mt, resp.data, resp.err

	case <-time.After(timeout + (100 * time.Millisecond)):
		return 0, nil, fmt.Errorf("timed out waiting for websocket read to complete")
	}
}

func (s *simpleSocket) ReadWorkerMsg(timeouts ...time.Duration) (workerMsg, error) {
	var msg workerMsg
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
	select {
	case err := <-req.resp:
		return err

	case <-time.After(timeout + (100 * time.Millisecond)):
		return fmt.Errorf("timed out waiting for websocket write to complete")
	}
}

func (s *simpleSocket) WriteWorkerMsg(msg workerMsg, timeouts ...time.Duration) error {
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
	// try to read a message
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
	s.logger.Debug("Closing WebSocket connection")
	if s.flushOnStop {
		remaining := len(s.outbound)
		s.logger.Debug("Flushing messages", "remaining", remaining)
		for i := 0; i < remaining; i++ {
			s.handleWrite(<-s.outbound)
		}
		s.logger.Debug("Finished flushing remaining messages")
	}
	if s.sendCloseMessage {
		_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		s.logger.Debug("Wrote WebSocket close message")
	}
	if s.waitForRemoteClose {
		s.logger.Debug("Waiting for remote end to close the connection")
		_ = s.conn.SetReadDeadline(time.Now().Add(s.remoteCloseWaitTimeout))
		_, _, err := s.conn.ReadMessage()
		s.logger.Debug("Wait complete", "err", err)
	}
}
