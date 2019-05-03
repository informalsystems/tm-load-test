package loadtest

import (
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
)

// Summary allows us to summarize the results of a master or slave node's
// operation.
type Summary struct {
	Interactions  int64         // How many interactions happened during testing.
	TotalTestTime time.Duration // How long the test took.
}

// Log will output the given test summary using the specified logger.
func (s *Summary) Log(logger logging.Logger) {
	logger.Info("Load test summary", "interactions", s.Interactions, "totalTestTime", s.TotalTestTime)
}
