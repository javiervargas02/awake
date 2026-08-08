// Package update checks whether a newer release of Awake exists.
//
// It implements ADR-0005 and ADR-0009. Its scope is a prohibition as much as a
// description: it fetches one document, compares two version strings, caches
// the answer and reports it. It never downloads a binary, never installs
// anything, never blocks a command, and never sends anything about the machine
// or the user.
package update

import (
	"errors"
	"fmt"
)

// ManifestSchemaVersion is the manifest format this build understands.
const ManifestSchemaVersion = 1

// DefaultChannel is the only channel in v0.1.0.
const DefaultChannel = "stable"

// Severity says how much a release matters.
//
// v0.1.0 carries, stores and displays this and enforces nothing: blocking new
// sessions on a security release is reserved for v0.2, along with exit code 4.
// The schema is the expensive part to change later; the policy needs a real
// release to have been made before it means anything.
type Severity string

const (
	SeverityOptional    Severity = "optional"
	SeverityRecommended Severity = "recommended"
	SeverityRequired    Severity = "required"
	SeveritySecurity    Severity = "security"
)

func (s Severity) Valid() bool {
	switch s {
	case SeverityOptional, SeverityRecommended, SeverityRequired, SeveritySecurity:
		return true
	default:
		return false
	}
}

func (s Severity) String() string { return string(s) }

// Manifest is the document served over HTTPS.
//
// It is untrusted input: unknown fields are ignored, a missing channel is not
// a crash, and nothing in it can influence anything but a printed sentence.
type Manifest struct {
	SchemaVersion int                `json:"schema_version"`
	Channels      map[string]Release `json:"channels"`
}

// Release describes the newest version on one channel.
type Release struct {
	Version  string   `json:"version"`
	Severity Severity `json:"severity"`
	Released string   `json:"released"`
	NotesURL string   `json:"notes_url"`
}

// ErrUnknownSchema means the manifest is newer than this build understands.
//
// This is not a failure to run: refusing to work because a *notification*
// format changed would be absurd. It is reported and ignored.
var ErrUnknownSchema = errors.New("manifest schema is newer than this version understands")

// Channel returns one channel's release, falling back to stable when the
// requested channel is absent — per ADR-0007's per-key degradation.
func (m Manifest) Channel(name string) (Release, string, error) {
	if m.SchemaVersion > ManifestSchemaVersion {
		return Release{}, name, fmt.Errorf("%w: manifest is version %d, this build understands %d",
			ErrUnknownSchema, m.SchemaVersion, ManifestSchemaVersion)
	}

	if release, ok := m.Channels[name]; ok {
		return release, name, nil
	}
	if release, ok := m.Channels[DefaultChannel]; ok && name != DefaultChannel {
		return release, DefaultChannel, nil
	}
	return Release{}, name, fmt.Errorf("manifest has no %q channel", name)
}
