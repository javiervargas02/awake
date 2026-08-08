package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ErrUnreadable means the file exists but could not be parsed or read. The
// returned Config is still usable — it is the defaults — so the caller can
// carry on and decide separately whether to quarantine the file.
var ErrUnreadable = errors.New("config file could not be read")

// Source records where the effective configuration came from.
type Source string

const (
	SourceFile     Source = "file"
	SourceDefaults Source = "defaults"
)

// Reason explains why a key fell back to its default.
type Reason string

const (
	ReasonMissingFile  Reason = "missing_file"
	ReasonInvalidValue Reason = "invalid_value"
	ReasonUnreadable   Reason = "unreadable"
)

// Defaulted records one key that did not survive loading.
type Defaulted struct {
	Key    string
	Reason Reason
	Detail string
}

// Implausible records one key whose value parsed but looks unintended.
type Implausible struct {
	Key    string
	Value  string
	Detail string
}

// Report describes what happened during loading, in enough detail to emit the
// config.loaded, config.defaulted and config.unknown_key events without the
// caller having to re-derive anything.
type Report struct {
	Source      Source
	Path        string
	Defaulted   []Defaulted
	UnknownKeys []string
	Implausible []Implausible
}

// raw mirrors the file's shape with pointer fields, so that "absent" is
// distinguishable from "present and zero". Durations arrive as strings and are
// parsed per key, which is what makes per-key degradation possible: decoding
// straight into typed fields would let one bad value reject the whole file.
type raw struct {
	Session struct {
		DefaultDuration *string `toml:"default_duration"`
	} `toml:"session"`
	Updates struct {
		Enabled       *bool   `toml:"enabled"`
		CheckInterval *string `toml:"check_interval"`
	} `toml:"updates"`
}

// Load reads the configuration at path.
//
// It always returns a usable Config. The error is informational: it reports
// that the file could not be understood, having already fallen back to
// defaults. Callers log the Report and continue.
func Load(path string) (Config, Report, error) {
	cfg := Defaults()
	report := Report{Source: SourceFile, Path: path}

	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Absence is normal, not a fault: running with no config file at all
		// is a healthy state.
		report.Source = SourceDefaults
		return cfg, report, nil
	case err != nil:
		report.Source = SourceDefaults
		report.Defaulted = append(report.Defaulted, Defaulted{
			Key: "*", Reason: ReasonUnreadable, Detail: err.Error(),
		})
		return cfg, report, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}

	var parsed raw
	meta, err := toml.Decode(string(data), &parsed)
	if err != nil {
		report.Source = SourceDefaults
		report.Defaulted = append(report.Defaulted, Defaulted{
			Key: "*", Reason: ReasonUnreadable, Detail: err.Error(),
		})
		return cfg, report, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}

	// A user running an older binary against a newer config, or with a typo,
	// gets a diagnosable warning and a working tool (ADR-0007).
	report.UnknownKeys = leafKeys(meta.Undecoded())

	if parsed.Session.DefaultDuration != nil {
		cfg.Session.DefaultDuration = resolveDuration(
			"session.default_duration", *parsed.Session.DefaultDuration,
			cfg.Session.DefaultDuration, &report)
	}
	if parsed.Updates.CheckInterval != nil {
		cfg.Updates.CheckInterval = resolveDuration(
			"updates.check_interval", *parsed.Updates.CheckInterval,
			cfg.Updates.CheckInterval, &report)
	}
	if parsed.Updates.Enabled != nil {
		cfg.Updates.Enabled = *parsed.Updates.Enabled
	}

	return cfg, report, nil
}

// leafKeys reduces the parser's list of undecoded keys to the ones a user
// would recognise as keys.
//
// An unrecognised table is reported alongside every key inside it, so
// "[future] setting = ..." arrives as both "future" and "future.setting".
// Telling someone about the table as well as its contents is noise; only the
// leaf is actionable.
func leafKeys(keys []toml.Key) []string {
	all := make([]string, 0, len(keys))
	for _, key := range keys {
		all = append(all, key.String())
	}

	leaves := make([]string, 0, len(all))
	for _, candidate := range all {
		isParent := false
		for _, other := range all {
			if other != candidate && strings.HasPrefix(other, candidate+".") {
				isParent = true
				break
			}
		}
		if !isParent {
			leaves = append(leaves, candidate)
		}
	}
	return leaves
}

// resolveDuration parses one duration key, falling back to its default if the
// value is unusable and warning if it is merely improbable.
func resolveDuration(key, value string, fallback time.Duration, report *Report) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		report.Defaulted = append(report.Defaulted, Defaulted{
			Key: key, Reason: ReasonInvalidValue, Detail: err.Error(),
		})
		return fallback
	}
	if parsed <= 0 {
		report.Defaulted = append(report.Defaulted, Defaulted{
			Key: key, Reason: ReasonInvalidValue, Detail: "must be positive",
		})
		return fallback
	}

	if r, ok := ranges[key]; ok && (parsed < r.min || parsed > r.max) {
		report.Implausible = append(report.Implausible, Implausible{
			Key:   key,
			Value: parsed.String(),
			Detail: fmt.Sprintf("outside the expected range %s to %s; honoured, but it looks unintended",
				r.min, r.max),
		})
	}

	return parsed
}
