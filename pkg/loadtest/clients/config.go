package clients

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/interchainio/tm-load-test/pkg/timeutils"
)

// Config encapsulates configuration for load testing clients.
type Config struct {
	Type               string                      `toml:"type"`                // The type of client to spawn.
	AdditionalParams   string                      `toml:"additional_params"`   // Additional parameters specific to the client type.
	Spawn              int                         `toml:"spawn"`               // The number of clients to spawn, per slave.
	SpawnRate          float64                     `toml:"spawn_rate"`          // The rate at which to spawn clients, per second, on each slave.
	MaxInteractions    int                         `toml:"max_interactions"`    // The maximum number of interactions emanating from each client.
	MaxTestTime        timeutils.ParseableDuration `toml:"max_test_time"`       // The maximum duration of the test, beyond which this client must be stopped.
	RequestWaitMin     timeutils.ParseableDuration `toml:"request_wait_min"`    // The minimum wait period before each request before sending another one.
	RequestWaitMax     timeutils.ParseableDuration `toml:"request_wait_max"`    // The maximum wait period before each request before sending another one.
	RequestTimeout     timeutils.ParseableDuration `toml:"request_timeout"`     // The maximum time allowed before considering a request to have timed out.
	InteractionTimeout timeutils.ParseableDuration `toml:"interaction_timeout"` // The maximum time allowed for an overall interaction.
}

// Validate makes sure the client configuration makes sense.
func (c *Config) Validate() error {
	if ct := GetClientType(c.Type); ct == nil {
		return fmt.Errorf("client type is unrecognized (supported: %s)", strings.Join(GetSupportedClientTypes(), ","))
	}
	if c.Spawn < 1 {
		return fmt.Errorf("client spawn count must be greater than 0")
	}
	if c.SpawnRate <= 0 {
		return fmt.Errorf("client spawn rate must be a positive floating point number")
	}
	if c.MaxInteractions == -1 {
		if c.MaxTestTime == 0 {
			return fmt.Errorf("if client max interactions is -1, max test time must be set")
		}
	} else if c.MaxInteractions < 1 {
		return fmt.Errorf("client maximum interactions must be -1, or greater than 1")
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("client request timeout cannot be 0")
	}
	return nil
}

// SpawnRateAndDelay returns the number of clients to spawn in a particular
// spawn batch, and the duration to wait after spawning before spawning the next
// batch.
func (c *Config) SpawnRateAndDelay() (int, time.Duration) {
	rate := int(math.Round(c.SpawnRate))
	// by default, we wait 1 second after each client batch spawning
	delay := int64(1)
	if 0.0 < c.SpawnRate && c.SpawnRate < 1.0 {
		rate = 1
		delay = int64(math.Round(float64(1.0) / c.SpawnRate))
	}
	return rate, time.Duration(delay) * time.Second
}
