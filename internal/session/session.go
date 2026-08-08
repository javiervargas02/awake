// Package session defines the core domain object of Awake.
//
// ADR-0002 makes the session the entity everything else is organised around:
// modes, the platform layer, logging and state all key off it. This package
// owns the shape of a session and the rules for how one may change; it does
// no I/O, spawns no processes, and reads no clock of its own.
package session

import (
	"errors"
	"fmt"
	"time"
)

// RecordVersion is the on-disk format version of a session record.
//
// The session file is NOT public API (see the state-and-repair architecture);
// this field exists for our own forward-compatibility, not as a promise to
// consumers. The supported way to read session state is `awake status --json`.
const RecordVersion = 1

var (
	// ErrInvalidDuration: a duration was zero, negative, or supplied alongside
	// --indefinite.
	ErrInvalidDuration = errors.New("duration must be positive")
	// ErrInvalidMode: an unknown mode was requested.
	ErrInvalidMode = errors.New("unknown mode")
	// ErrAlreadyEnded: a terminal session cannot end twice.
	ErrAlreadyEnded = errors.New("session has already ended")
	// ErrInvalidEndReason: the reason does not match the transition.
	ErrInvalidEndReason = errors.New("invalid end reason for this transition")
)

// Session is one bounded, observable stay-awake session.
//
// A session is created running and ends exactly once. Deadline is nil for an
// indefinite session, and EndedAt/EndReason are empty until it ends.
type Session struct {
	RecordVersion int    `json:"record_version"`
	ID            ID     `json:"id"`
	AppVersion    string `json:"app_version"`
	Mode          Mode   `json:"mode"`

	RequestedDuration Duration   `json:"requested_duration"`
	Indefinite        bool       `json:"indefinite"`
	Deadline          *time.Time `json:"deadline"`

	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`

	Status    Status    `json:"status"`
	EndReason EndReason `json:"end_reason,omitempty"`

	// Diagnostics. ADR-0008 makes the advisory lock authoritative for
	// liveness; these explain what happened, they do not decide it.
	OwnerPID       int        `json:"owner_pid"`
	OwnerStartedAt *time.Time `json:"owner_started_at,omitempty"`
	MechanismPID   int        `json:"mechanism_pid,omitempty"`
}

// Params describes a requested session.
type Params struct {
	Mode       Mode
	Duration   time.Duration
	Indefinite bool
	AppVersion string
	OwnerPID   int
	// OwnerStartedAt is optional: a platform that cannot report a process
	// start time loses diagnostic precision and nothing more.
	OwnerStartedAt *time.Time
}

// New creates a running session starting at now.
//
// The deadline is computed once, here, as an absolute instant, and is never
// recomputed (ADR-0002). A session that outlives a system suspend is compared
// against this instant on waking, rather than being extended to compensate.
func New(now time.Time, p Params) (*Session, error) {
	if !p.Mode.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidMode, p.Mode)
	}

	switch {
	case p.Indefinite && p.Duration != 0:
		return nil, fmt.Errorf("%w: a duration cannot be combined with an indefinite session", ErrInvalidDuration)
	case !p.Indefinite && p.Duration <= 0:
		return nil, fmt.Errorf("%w: got %s", ErrInvalidDuration, p.Duration)
	}

	now = now.UTC()

	id, err := NewID(now)
	if err != nil {
		return nil, err
	}

	s := &Session{
		RecordVersion:     RecordVersion,
		ID:                id,
		AppVersion:        p.AppVersion,
		Mode:              p.Mode,
		RequestedDuration: Duration(p.Duration),
		Indefinite:        p.Indefinite,
		StartedAt:         now,
		Status:            StatusRunning,
		OwnerPID:          p.OwnerPID,
		OwnerStartedAt:    utcOrNil(p.OwnerStartedAt),
	}

	if !p.Indefinite {
		deadline := now.Add(p.Duration)
		s.Deadline = &deadline
	}

	return s, nil
}

// Complete ends the session because its deadline was reached.
func (s *Session) Complete(now time.Time) error {
	if s.Status.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrAlreadyEnded, s.Status)
	}
	if s.Indefinite {
		return fmt.Errorf("%w: an indefinite session has no deadline to elapse", ErrInvalidEndReason)
	}
	s.end(now, StatusCompleted, ReasonDurationElapsed)
	return nil
}

// Stop ends the session at the user's request. The reason distinguishes an
// explicit `awake stop` from an interrupt; both are the user, one is louder.
func (s *Session) Stop(now time.Time, reason EndReason) error {
	if s.Status.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrAlreadyEnded, s.Status)
	}
	if reason != ReasonUserStopped && reason != ReasonInterrupted {
		return fmt.Errorf("%w: %q is not a stop reason", ErrInvalidEndReason, reason)
	}
	s.end(now, StatusStopped, reason)
	return nil
}

// Fail ends the session because something went wrong.
//
// For ReasonCrashed, now is the moment of *discovery*, not of death: Awake
// does not know when the owning process died and must not invent a timestamp.
func (s *Session) Fail(now time.Time, reason EndReason) error {
	if s.Status.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrAlreadyEnded, s.Status)
	}
	if reason != ReasonModeFailure && reason != ReasonCrashed {
		return fmt.Errorf("%w: %q is not a failure reason", ErrInvalidEndReason, reason)
	}
	s.end(now, StatusFailed, reason)
	return nil
}

func (s *Session) end(now time.Time, status Status, reason EndReason) {
	ended := now.UTC()
	s.EndedAt = &ended
	s.Status = status
	s.EndReason = reason
}

// Elapsed reports how long the session has run, or ran.
func (s *Session) Elapsed(now time.Time) time.Duration {
	if s.EndedAt != nil {
		return s.EndedAt.Sub(s.StartedAt)
	}
	return now.UTC().Sub(s.StartedAt)
}

// Remaining reports the time left before the deadline. The boolean is false
// for an indefinite session, which has no remaining time — callers must
// render that as "no scheduled end" rather than as zero.
func (s *Session) Remaining(now time.Time) (time.Duration, bool) {
	if s.Deadline == nil {
		return 0, false
	}
	remaining := s.Deadline.Sub(now.UTC())
	if remaining < 0 {
		return 0, true
	}
	return remaining, true
}

// DeadlinePassed reports whether the session's end time has arrived.
func (s *Session) DeadlinePassed(now time.Time) bool {
	return s.Deadline != nil && !now.UTC().Before(*s.Deadline)
}

// Overrun reports how far past its deadline a session was *noticed* to have
// ended. It is non-zero when the machine slept through the deadline, and it
// is what makes a late ending explainable instead of suspicious.
func (s *Session) Overrun(now time.Time) time.Duration {
	if s.Deadline == nil {
		return 0
	}
	at := now.UTC()
	if s.EndedAt != nil {
		at = *s.EndedAt
	}
	if overrun := at.Sub(*s.Deadline); overrun > 0 {
		return overrun
	}
	return 0
}

// Validate checks a session for internal consistency. It is used when reading
// a record from disk, which a user may have edited or an older version may
// have written.
func (s *Session) Validate() error {
	switch {
	case !s.ID.Valid():
		return fmt.Errorf("invalid session id %q", s.ID)
	case !s.Mode.Valid():
		return fmt.Errorf("%w: %q", ErrInvalidMode, s.Mode)
	case !s.Status.Valid():
		return fmt.Errorf("invalid status %q", s.Status)
	case s.StartedAt.IsZero():
		return errors.New("missing start time")
	case s.Indefinite && s.Deadline != nil:
		return errors.New("indefinite session must not have a deadline")
	case !s.Indefinite && s.Deadline == nil:
		return errors.New("bounded session must have a deadline")
	}

	if s.Status.IsTerminal() {
		if s.EndedAt == nil {
			return fmt.Errorf("status %q requires an end time", s.Status)
		}
		if !s.EndReason.Valid() {
			return fmt.Errorf("status %q requires a valid end reason, got %q", s.Status, s.EndReason)
		}
		if s.EndedAt.Before(s.StartedAt) {
			return errors.New("end time precedes start time")
		}
	} else if s.EndedAt != nil || s.EndReason != "" {
		return errors.New("running session must not have an end time or reason")
	}

	return nil
}

func utcOrNil(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
