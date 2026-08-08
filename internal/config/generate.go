package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// template is the file Awake writes when no config exists.
//
// It is commented because a regenerated config is a teaching surface, not just
// a set of values: the best moment to explain what a key does is inside the
// file the user is about to edit. This is the reason ADR-0007 chose a format
// that supports comments at all.
//
// Every key is written commented-out at its default, so the generated file
// documents the defaults without overriding them — which keeps Defaults() the
// single source of truth even after the file exists.
const template = `# Awake configuration
#
# Every setting below is optional. Deleting this file, or any part of it, is
# safe: Awake falls back to the built-in defaults shown here and says so in
# its logs.
#
# Awake never edits this file. If you change it, your comments and formatting
# stay exactly as you left them.
#
# Durations use Go's format: "45s", "30m", "1h30m", "24h".

[session]
# How long a session lasts when you run 'awake start' with no duration.
# Default: %s
# default_duration = "%s"

[updates]
# Whether Awake checks for new releases. When false, Awake makes no network
# requests at all.
# Default: %t
# enabled = %t

# The minimum time between update checks. Results are cached, so no command
# ever waits on the network.
# Default: %s
# check_interval = "%s"
`

// Render returns the contents of a default configuration file.
func Render() string {
	d := Defaults()
	return fmt.Sprintf(template,
		d.Session.DefaultDuration, d.Session.DefaultDuration,
		d.Updates.Enabled, d.Updates.Enabled,
		d.Updates.CheckInterval, d.Updates.CheckInterval,
	)
}

// Generate writes a default configuration file.
//
// It refuses to overwrite an existing file. Awake creates a config when none
// exists and never rewrites one the user owns (ADR-0007); replacing a file
// here would break that promise even when called by repair.
func Generate(path string, perm os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("refusing to overwrite existing config at %s", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking for existing config at %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(Render()), perm); err != nil {
		return fmt.Errorf("writing config to %s: %w", path, err)
	}
	return nil
}

// Keys returns every recognised configuration key, for help text and for the
// test that keeps the generated template in step with the loader.
func Keys() []string {
	return []string{
		"session.default_duration",
		"updates.enabled",
		"updates.check_interval",
	}
}

func containsKey(text, key string) bool {
	return strings.Contains(text, key)
}
