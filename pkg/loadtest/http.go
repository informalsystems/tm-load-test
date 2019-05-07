package loadtest

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
)

const defaultBaseServerMaxStartupTime = 100 * time.Millisecond
const defaultBaseServerMaxShutdownTime = 1 * time.Second

type baseServer struct {
	logger logging.Logger
	svr    *http.Server

	stoppedc chan struct{}

	mtx         sync.Mutex
	flagStarted bool
}

func newBaseServer(addr string, mux *http.ServeMux, logger logging.Logger) (*baseServer, error) {
	resolvedAddr, err := resolveBindAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve HTTP server bind address (%s): %v", addr, err)
	}
	logger.Debug("Resolved HTTP server address", "addr", resolvedAddr)
	return &baseServer{
		logger: logger,
		svr: &http.Server{
			Addr:    resolvedAddr,
			Handler: mux,
		},
		stoppedc: make(chan struct{}),
	}, nil
}

func (s *baseServer) setStarted() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.flagStarted = true
}

func (s *baseServer) isStarted() bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.flagStarted
}

func (s *baseServer) start() error {
	errc := make(chan error, 1)
	go func() {
		if err := s.svr.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error("Failed to start HTTP server", "err", err)
			errc <- NewError(ErrSlaveFailed, err, "failed to start HTTP server")
		} else {
			s.logger.Info("Successfully shut down HTTP server")
		}
		close(s.stoppedc)
	}()

	// wait for the server to start
	select {
	case err := <-errc:
		return err

	case <-time.After(defaultBaseServerMaxStartupTime):
	}

	s.setStarted()
	s.logger.Info("Started HTTP server", s.svr.Addr)
	return nil
}

func (s *baseServer) shutdown() error {
	if !s.isStarted() {
		return nil
	}
	err := s.svr.Shutdown(context.Background())
	select {
	case <-s.stoppedc:
		return err

	case <-time.After(defaultBaseServerMaxShutdownTime):
		return NewError(ErrSlaveFailed, nil, "failed to shut down HTTP server within time limit")
	}
}

//
//-----------------------------------------------------------------------------
//

// longPoll is a simple, relatively generic long-polling operation that attempts
// to keep sending the given request until it gets a positive response, or if
// the server responds with a failure status code (>= 400), or if the routine
// runs for longer than `overallTimeout`, or if the `cancel` function returns
// true. Returns the body of the response (as a byte array), as well as any
// error that may have occurred during polling.
func longPoll(req *http.Request, singlePollTimeout, overallTimeout time.Duration, cancel func() bool, logger logging.Logger) ([]byte, error) {
	client := &http.Client{
		Timeout: singlePollTimeout,
	}
	startTime := time.Now()
	lastPoll := startTime
	attempt := 0
	for {
		logger.Debug("Sending request", "attempt", attempt)
		res, err := client.Do(req)
		if err == nil {
			defer res.Body.Close()
			logger.Debug("Got response", "code", res.StatusCode)
			if res.StatusCode == 200 {
				body, err := ioutil.ReadAll(res.Body)
				if err != nil {
					return nil, err
				}
				// all's good
				return body, nil
			} else if res.StatusCode >= 400 {
				return nil, fmt.Errorf("request failed with status code %d", res.StatusCode)
			} else {
				logger.Debug("Server not ready yet")
			}
		} else {
			logger.Debug("Failed to send request, will try again", "err", err)
			if time.Since(startTime) >= overallTimeout {
				return nil, fmt.Errorf("failed to hear back from server within %s", overallTimeout.String())
			}
		}
		// check if we need to cancel now
		if cancel() {
			return nil, fmt.Errorf("polling cancelled")
		}
		// to prevent polling again too quickly
		if time.Since(lastPoll) < time.Second {
			time.Sleep(time.Second)
		}
		lastPoll = time.Now()
		attempt++
	}
}

func jsonResponse(w http.ResponseWriter, msg interface{}, code int) {
	m := msg
	msgString, isString := msg.(string)
	if isString {
		m = resMessage{Message: msgString}
	}
	s, err := toJSON(&m)
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(s)); err != nil {
		http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
	}
}
