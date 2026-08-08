package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// base is a fixed instant. Tests never read the real clock, so they are
// deterministic and instant regardless of how long a session "lasts".
var base = time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)

func mustNew(t *testing.T, p Params) *Session {
	t.Helper()
	s, err := New(base, p)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return s
}

func boundedParams(d time.Duration) Params {
	return Params{Mode: ModeSystem, Duration: d, AppVersion: "test", OwnerPID: 42}
}

func TestNewValidation(t *testing.T) {
	cases := []struct {
		name    string
		params  Params
		wantErr error
	}{
		{"bounded session", boundedParams(30 * time.Minute), nil},
		{"indefinite session", Params{Mode: ModeSystem, Indefinite: true}, nil},
		{"zero duration", boundedParams(0), ErrInvalidDuration},
		{"negative duration", boundedParams(-time.Minute), ErrInvalidDuration},
		{"duration with indefinite", Params{Mode: ModeSystem, Duration: time.Hour, Indefinite: true}, ErrInvalidDuration},
		{"unknown mode", Params{Mode: "telepathy", Duration: time.Hour}, ErrInvalidMode},
		{"empty mode", Params{Duration: time.Hour}, ErrInvalidMode},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(base, tc.params)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewSetsDeadlineOnce(t *testing.T) {
	s := mustNew(t, boundedParams(30*time.Minute))

	if s.Deadline == nil {
		t.Fatal("bounded session has no deadline")
	}
	if want := base.Add(30 * time.Minute); !s.Deadline.Equal(want) {
		t.Errorf("deadline = %v, want %v", s.Deadline, want)
	}
	if s.Status != StatusRunning {
		t.Errorf("status = %q, want %q", s.Status, StatusRunning)
	}
	if s.EndReason != "" {
		t.Errorf("new session has end reason %q, want empty", s.EndReason)
	}
}

func TestIndefiniteHasNoDeadline(t *testing.T) {
	s := mustNew(t, Params{Mode: ModeSystem, Indefinite: true})

	if s.Deadline != nil {
		t.Fatalf("indefinite session has deadline %v", s.Deadline)
	}
	if _, ok := s.Remaining(base); ok {
		t.Error("Remaining() reported a value for an indefinite session; callers must be able to tell")
	}
	if err := s.Complete(base); !errors.Is(err, ErrInvalidEndReason) {
		t.Errorf("Complete() on indefinite session = %v, want ErrInvalidEndReason", err)
	}
}

func TestTransitions(t *testing.T) {
	cases := []struct {
		name       string
		transition func(*Session) error
		wantStatus Status
		wantReason EndReason
	}{
		{"complete", func(s *Session) error { return s.Complete(base.Add(time.Hour)) },
			StatusCompleted, ReasonDurationElapsed},
		{"user stopped", func(s *Session) error { return s.Stop(base.Add(time.Minute), ReasonUserStopped) },
			StatusStopped, ReasonUserStopped},
		{"interrupted", func(s *Session) error { return s.Stop(base.Add(time.Minute), ReasonInterrupted) },
			StatusStopped, ReasonInterrupted},
		{"mode failure", func(s *Session) error { return s.Fail(base.Add(time.Minute), ReasonModeFailure) },
			StatusFailed, ReasonModeFailure},
		{"crashed", func(s *Session) error { return s.Fail(base.Add(time.Minute), ReasonCrashed) },
			StatusFailed, ReasonCrashed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustNew(t, boundedParams(30*time.Minute))

			if err := tc.transition(s); err != nil {
				t.Fatalf("transition error = %v", err)
			}
			if s.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", s.Status, tc.wantStatus)
			}
			if s.EndReason != tc.wantReason {
				t.Errorf("end reason = %q, want %q", s.EndReason, tc.wantReason)
			}
			if s.EndedAt == nil {
				t.Fatal("terminal session has no end time")
			}
			if err := s.Validate(); err != nil {
				t.Errorf("terminal session failed validation: %v", err)
			}
		})
	}
}

// A session ends exactly once. This guards the state machine against a
// shutdown path running twice, which is plausible when a deadline and a signal
// arrive together.
func TestSessionEndsOnlyOnce(t *testing.T) {
	cases := []struct {
		name  string
		again func(*Session) error
	}{
		{"complete again", func(s *Session) error { return s.Complete(base.Add(2 * time.Hour)) }},
		{"stop after complete", func(s *Session) error { return s.Stop(base.Add(2*time.Hour), ReasonUserStopped) }},
		{"fail after complete", func(s *Session) error { return s.Fail(base.Add(2*time.Hour), ReasonModeFailure) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustNew(t, boundedParams(time.Minute))
			if err := s.Complete(base.Add(time.Minute)); err != nil {
				t.Fatalf("first Complete() error = %v", err)
			}

			endedAt := *s.EndedAt
			if err := tc.again(s); !errors.Is(err, ErrAlreadyEnded) {
				t.Fatalf("second transition = %v, want ErrAlreadyEnded", err)
			}
			if !s.EndedAt.Equal(endedAt) {
				t.Error("rejected transition mutated the end time")
			}
			if s.EndReason != ReasonDurationElapsed {
				t.Errorf("rejected transition changed end reason to %q", s.EndReason)
			}
		})
	}
}

func TestWrongReasonRejected(t *testing.T) {
	cases := []struct {
		name       string
		transition func(*Session) error
	}{
		{"stop with failure reason", func(s *Session) error { return s.Stop(base, ReasonModeFailure) }},
		{"stop with elapsed reason", func(s *Session) error { return s.Stop(base, ReasonDurationElapsed) }},
		{"fail with stop reason", func(s *Session) error { return s.Fail(base, ReasonUserStopped) }},
		{"stop with reserved input reason", func(s *Session) error { return s.Stop(base, ReasonInputDetected) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustNew(t, boundedParams(time.Hour))
			if err := tc.transition(s); !errors.Is(err, ErrInvalidEndReason) {
				t.Errorf("transition = %v, want ErrInvalidEndReason", err)
			}
			if s.Status != StatusRunning {
				t.Errorf("rejected transition changed status to %q", s.Status)
			}
		})
	}
}

// The machine-slept-through-its-deadline case from ADR-0002: the session is
// not extended to compensate, and the lateness is recorded.
func TestSleepPastDeadlineRecordsOverrun(t *testing.T) {
	s := mustNew(t, boundedParams(30*time.Minute))

	// The machine suspends and wakes two hours later.
	wake := base.Add(2 * time.Hour)

	if !s.DeadlinePassed(wake) {
		t.Fatal("deadline should have passed while the machine slept")
	}
	if remaining, ok := s.Remaining(wake); !ok || remaining != 0 {
		t.Errorf("Remaining() = %v, %v; want 0, true (never negative)", remaining, ok)
	}

	if err := s.Complete(wake); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if want := 90 * time.Minute; s.Overrun(wake) != want {
		t.Errorf("overrun = %v, want %v", s.Overrun(wake), want)
	}
	if want := 2 * time.Hour; s.Elapsed(wake) != want {
		t.Errorf("elapsed = %v, want %v (the session is not extended)", s.Elapsed(wake), want)
	}
}

func TestOverrunZeroWhenOnTime(t *testing.T) {
	s := mustNew(t, boundedParams(30*time.Minute))
	at := base.Add(30 * time.Minute)

	if err := s.Complete(at); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if got := s.Overrun(at); got != 0 {
		t.Errorf("overrun = %v, want 0 for an on-time ending", got)
	}
}

func TestValidateRejectsInconsistentRecords(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Session)
	}{
		{"running with end time", func(s *Session) { s.EndedAt = &base }},
		{"running with end reason", func(s *Session) { s.EndReason = ReasonUserStopped }},
		{"terminal without end time", func(s *Session) { s.Status = StatusCompleted }},
		{"unknown status", func(s *Session) { s.Status = "sleeping" }},
		{"unknown mode", func(s *Session) { s.Mode = "telepathy" }},
		{"malformed id", func(s *Session) { s.ID = "not-an-id" }},
		{"bounded without deadline", func(s *Session) { s.Deadline = nil }},
		{"indefinite with deadline", func(s *Session) { s.Indefinite = true }},
		{"missing start time", func(s *Session) { s.StartedAt = time.Time{} }},
		{"end before start", func(s *Session) {
			before := base.Add(-time.Hour)
			s.Status, s.EndReason, s.EndedAt = StatusCompleted, ReasonDurationElapsed, &before
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := mustNew(t, boundedParams(time.Hour))
			tc.mutate(s)
			if err := s.Validate(); err == nil {
				t.Error("Validate() accepted an inconsistent record")
			}
		})
	}
}

func TestValidateAcceptsHealthyRecords(t *testing.T) {
	running := mustNew(t, boundedParams(time.Hour))
	if err := running.Validate(); err != nil {
		t.Errorf("running session rejected: %v", err)
	}

	indefinite := mustNew(t, Params{Mode: ModeSystem, Indefinite: true})
	if err := indefinite.Validate(); err != nil {
		t.Errorf("indefinite session rejected: %v", err)
	}
}

// The record must survive a round trip through disk, and must stay readable
// to a human doing so (principle 3).
func TestJSONRoundTrip(t *testing.T) {
	original := mustNew(t, boundedParams(30*time.Minute))
	if err := original.Stop(base.Add(10*time.Minute), ReasonUserStopped); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if !strings.Contains(string(encoded), `"requested_duration":"30m0s"`) {
		t.Errorf("duration is not human-readable in %s", encoded)
	}

	var decoded Session
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if err := decoded.Validate(); err != nil {
		t.Errorf("decoded session failed validation: %v", err)
	}
	if decoded.ID != original.ID ||
		decoded.Status != original.Status ||
		decoded.EndReason != original.EndReason ||
		decoded.RequestedDuration != original.RequestedDuration {
		t.Errorf("round trip changed the record:\n got %+v\nwant %+v", decoded, original)
	}
	if !decoded.Deadline.Equal(*original.Deadline) {
		t.Errorf("deadline changed: got %v, want %v", decoded.Deadline, original.Deadline)
	}
}

func TestDurationAcceptsLegacyNanoseconds(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte("1800000000000"), &d); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if want := 30 * time.Minute; d.Std() != want {
		t.Errorf("duration = %v, want %v", d.Std(), want)
	}
}
