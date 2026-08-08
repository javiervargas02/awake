package session

// Status is a session's coarse, current state. ADR-0002 fixes these four
// values; three of them are terminal, and a session never leaves a terminal
// state.
//
// Status answers "what state is this in". EndReason answers "why did it end".
// They are separate because collapsing them would make a crash discovered
// after the fact impossible to represent.
type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusStopped   Status = "stopped"
	StatusFailed    Status = "failed"
)

// IsTerminal reports whether the session has ended.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusStopped, StatusFailed:
		return true
	default:
		return false
	}
}

func (s Status) Valid() bool {
	return s == StatusRunning || s.IsTerminal()
}

func (s Status) String() string { return string(s) }

// EndReason records why a session ended.
type EndReason string

const (
	// ReasonDurationElapsed: the deadline was reached.
	ReasonDurationElapsed EndReason = "duration_elapsed"
	// ReasonUserStopped: `awake stop` asked for it.
	ReasonUserStopped EndReason = "user_stopped"
	// ReasonInterrupted: Ctrl-C, or another interrupt.
	ReasonInterrupted EndReason = "interrupted"
	// ReasonModeFailure: the platform mechanism failed or died.
	ReasonModeFailure EndReason = "mode_failure"
	// ReasonCrashed: the owning process vanished; discovered afterwards.
	ReasonCrashed EndReason = "crashed"
	// ReasonInputDetected is RESERVED for end-on-input (v0.2). It is never
	// produced in v0.1.0; it exists so that adding the feature is an additive
	// change to the log schema rather than a breaking one.
	ReasonInputDetected EndReason = "input_detected"
)

func (r EndReason) Valid() bool {
	switch r {
	case ReasonDurationElapsed, ReasonUserStopped, ReasonInterrupted,
		ReasonModeFailure, ReasonCrashed, ReasonInputDetected:
		return true
	default:
		return false
	}
}

func (r EndReason) String() string { return string(r) }

// Mode is the strategy used to keep the machine awake. Only ModeSystem exists
// in v0.1.0; the type exists so that adding modes does not change the core.
type Mode string

const ModeSystem Mode = "system"

func (m Mode) Valid() bool { return m == ModeSystem }

func (m Mode) String() string { return string(m) }
