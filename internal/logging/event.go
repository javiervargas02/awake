// Package logging writes Awake's structured event log.
//
// It implements ADR-0004: JSON Lines, two destinations, a closed event
// vocabulary, and an envelope that is public API from v0.1.0. The bar it exists
// to meet is a testable one — given only a session's trace file, a reader must
// be able to reconstruct what Awake did, in order, and why the session ended.
package logging

import (
	"encoding/json"
	"time"
)

// SchemaVersion is the version of the envelope and event vocabulary.
//
// It increments only on a breaking change — a renamed or removed event or
// field, or a changed type or meaning. Adding an event, a field or a level is
// additive and leaves this alone.
const SchemaVersion = 1

// timestampLayout is RFC 3339 with microseconds. Microsecond precision costs
// nothing and makes ordering unambiguous in the global log, which has
// concurrent writers from independent processes.
const timestampLayout = "2006-01-02T15:04:05.000000Z07:00"

// Level classifies an event. Three levels only: info for normal operation,
// warn for handled degradation, error for something the user asked for that
// failed. There is no debug level, and no verbosity setting relaxes the
// privacy rules — nothing more is ever collected.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Fields carries the event-specific payload.
type Fields map[string]any

// envelope is the wire format. Event-specific fields live under `data`, never
// at the top level, so that adding an envelope field later can never collide
// with an event's payload.
//
// There is deliberately no free-text message field: every event name is
// self-describing and every specific lives in a typed field. Human phrasing is
// a rendering concern, derived from this data by whoever displays it.
type envelope struct {
	TS            string `json:"ts"`
	SchemaVersion int    `json:"schema_version"`
	Level         Level  `json:"level"`
	Event         string `json:"event"`
	SessionID     string `json:"session_id,omitempty"`
	Data          Fields `json:"data,omitempty"`
}

// EnvelopeFields lists the reserved top-level keys. The privacy test uses this
// as part of its allowlist.
func EnvelopeFields() []string {
	return []string{"ts", "schema_version", "level", "event", "session_id", "data"}
}

func newEnvelope(now time.Time, level Level, event, sessionID string, data Fields) envelope {
	return envelope{
		TS:            now.UTC().Format(timestampLayout),
		SchemaVersion: SchemaVersion,
		Level:         level,
		Event:         event,
		SessionID:     sessionID,
		Data:          data,
	}
}

// encode renders one event as a single line, newline included.
//
// The whole event becomes one byte slice so that it can be written with one
// syscall: appended writes of a complete line cannot interleave into a corrupt
// line, which matters because the global log has multiple writers.
func (e envelope) encode() ([]byte, error) {
	line, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(line, '\n'), nil
}
