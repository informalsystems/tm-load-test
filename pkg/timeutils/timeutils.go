package timeutils

import (
	"encoding"
	"time"
)

// ParseableDuration represents a time.Duration that implements
// encoding.TextUnmarshaler.
type ParseableDuration time.Duration

// ParseableDuration implements encoding.TextUnmarshaler
var _ encoding.TextUnmarshaler = (*ParseableDuration)(nil)

// UnmarshalText allows us a convenient way to unmarshal durations.
func (d *ParseableDuration) UnmarshalText(text []byte) error {
	dur, err := time.ParseDuration(string(text))
	if err == nil {
		*d = ParseableDuration(dur)
	}
	return err
}

// Duration is a convenience method for converting this parseable duration into
// a standard time.Duration instance.
func (d ParseableDuration) Duration() time.Duration {
	return time.Duration(d)
}
