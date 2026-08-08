// Package config loads Awake's settings.
//
// It implements ADR-0007: TOML, optional, never authoritative, and degrading
// per key rather than failing. A bad config can make Awake behave differently;
// it can never stop Awake from working.
package config

import "time"

// Config is the resolved configuration. Every field always holds a usable
// value: callers never need to check for zero.
type Config struct {
	Session Session
	Updates Updates
}

type Session struct {
	// DefaultDuration is used when `awake start` is given no duration.
	DefaultDuration time.Duration
}

type Updates struct {
	Enabled       bool
	CheckInterval time.Duration
}

// Defaults returns the built-in configuration. This is the authoritative
// source of defaults: the file only ever overrides it, and a missing file is
// exactly equivalent to an empty one.
func Defaults() Config {
	return Config{
		Session: Session{
			DefaultDuration: 30 * time.Minute,
		},
		Updates: Updates{
			Enabled:       true,
			CheckInterval: 24 * time.Hour,
		},
	}
}

// Plausible ranges. A value outside its range is honoured — it is the user's
// machine — but reported as a warning, because a check interval of one second
// parses perfectly and is almost certainly a mistake.
var ranges = map[string]struct{ min, max time.Duration }{
	"session.default_duration": {min: time.Minute, max: 24 * time.Hour},
	"updates.check_interval":   {min: time.Hour, max: 30 * 24 * time.Hour},
}
