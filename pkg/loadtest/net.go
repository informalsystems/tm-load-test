package loadtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
)

// A simple, relatively generic long-polling operation that attempts to keep
// sending the given request until it gets a positive response, or if the server
// responds with a failure status code (>= 400), or if the routine runs for
// longer than `longPollTimeout`, or if the `cancel` function returns true.
func longPoll(req *http.Request, singlePollTimeout, longPollTimeout time.Duration, cancel func() bool, logger logging.Logger) (*http.Response, error) {
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
			logger.Debug("Got response", "code", res.StatusCode)
			if res.StatusCode == 200 {
				// all's good
				return res, nil
			} else if res.StatusCode >= 400 {
				return res, fmt.Errorf("request failed", "code", res.StatusCode)
			} else {
				logger.Debug("Server not ready yet")
			}
		} else {
			logger.Debug("Failed to send request, will try again", "err", err)
			if time.Since(startTime) >= longPollTimeout {
				return nil, fmt.Errorf("failed to hear back from server within %s", longPollTimeout.String())
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
		lastPoll := time.Now()
		attempt++
	}
}
