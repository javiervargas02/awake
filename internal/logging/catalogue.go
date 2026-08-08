package logging

// The event vocabulary is a closed, stable set (ADR-0004). New events may be
// added in a minor release; renaming or removing one is breaking and requires a
// major version plus a SchemaVersion increment.
const (
	EventAppStarted = "app.started"

	EventConfigLoaded     = "config.loaded"
	EventConfigDefaulted  = "config.defaulted"
	EventConfigUnknownKey = "config.unknown_key"

	EventSessionStartRefused = "session.start_refused"
	EventSessionCreated      = "session.created"
	EventSessionStarted      = "session.started"
	EventSessionCompleted    = "session.completed"
	EventSessionStopped      = "session.stopped"
	EventSessionFailed       = "session.failed"
	EventSessionRecovered    = "session.recovered"

	EventModeStarted = "mode.started"
	EventModeStopped = "mode.stopped"
	EventModeFailed  = "mode.failed"

	EventUpdateCheckStarted   = "update.check.started"
	EventUpdateCheckCompleted = "update.check.completed"
	EventUpdateAvailable      = "update.available"

	EventHealthCheckCompleted = "health.check.completed"
	EventRepairPerformed      = "repair.performed"

	// EventLogSinkFailed reports that a log destination could not be written.
	// It is the one event that exists to explain a gap in the others.
	EventLogSinkFailed = "log.sink_failed"
)

// Spec describes one event's contract: the fields it always carries, the ones
// it may carry, and whether it belongs to a session.
type Spec struct {
	Name          string
	Level         Level
	Required      []string
	Optional      []string
	SessionScoped bool
}

// Catalogue is the source of truth for the event vocabulary.
//
// The schema contract test asserts this list exactly, so a rename shows up as a
// diff that prompts a changelog entry rather than slipping through as a
// silently broken consumer. The privacy test derives its field allowlist from
// here, which means a new field cannot reach a log file without appearing in
// this list first.
func Catalogue() []Spec {
	return []Spec{
		{Name: EventAppStarted, Level: LevelInfo,
			Required: []string{"app_version", "command"}},

		{Name: EventConfigLoaded, Level: LevelInfo,
			Required: []string{"source"}},
		{Name: EventConfigDefaulted, Level: LevelWarn,
			Required: []string{"key", "reason"}},
		{Name: EventConfigUnknownKey, Level: LevelWarn,
			Required: []string{"key"}},

		{Name: EventSessionStartRefused, Level: LevelWarn,
			Required: []string{"reason"}},
		{Name: EventSessionCreated, Level: LevelInfo, SessionScoped: true,
			Required: []string{"app_version", "mode", "requested_duration", "indefinite", "deadline", "owner_pid"}},
		{Name: EventSessionStarted, Level: LevelInfo, SessionScoped: true},
		{Name: EventSessionCompleted, Level: LevelInfo, SessionScoped: true,
			Required: []string{"end_reason", "elapsed", "overrun"}},
		{Name: EventSessionStopped, Level: LevelInfo, SessionScoped: true,
			Required: []string{"end_reason", "elapsed"},
			Optional: []string{"remaining"}},
		{Name: EventSessionFailed, Level: LevelError, SessionScoped: true,
			Required: []string{"end_reason", "elapsed"},
			Optional: []string{"error"}},
		{Name: EventSessionRecovered, Level: LevelWarn, SessionScoped: true,
			Required: []string{"owner_pid", "discovered_at", "platform_reclaimed"}},

		{Name: EventModeStarted, Level: LevelInfo, SessionScoped: true,
			Required: []string{"mode", "mechanism", "mechanism_pid", "assertion_verified"}},
		{Name: EventModeStopped, Level: LevelInfo, SessionScoped: true,
			Required: []string{"mode"}},
		{Name: EventModeFailed, Level: LevelError, SessionScoped: true,
			Required: []string{"mode", "error"}},

		{Name: EventUpdateCheckStarted, Level: LevelInfo,
			Required: []string{"channel"}},
		{Name: EventUpdateCheckCompleted, Level: LevelInfo,
			Required: []string{"result"},
			Optional: []string{"latest_version", "error"}},
		{Name: EventUpdateAvailable, Level: LevelInfo,
			Required: []string{"current_version", "latest_version", "severity"}},

		{Name: EventHealthCheckCompleted, Level: LevelInfo,
			Required: []string{"total", "ok", "warnings", "problems", "findings"}},
		{Name: EventRepairPerformed, Level: LevelWarn,
			Required: []string{"action", "target", "result"}},

		{Name: EventLogSinkFailed, Level: LevelWarn,
			Required: []string{"sink", "error"}},
	}
}

// SpecFor returns the contract for one event name.
func SpecFor(name string) (Spec, bool) {
	for _, spec := range Catalogue() {
		if spec.Name == name {
			return spec, true
		}
	}
	return Spec{}, false
}
