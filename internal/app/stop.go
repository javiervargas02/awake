package app

import (
	"context"
	"errors"
	"syscall"
	"time"

	"github.com/javiervargas02/awake/internal/session"
	"github.com/javiervargas02/awake/internal/store"
)

// stopGrace is how long `awake stop` waits for a session to end.
//
// Fixed rather than configurable: a knob here invites tuning something that
// should simply work.
const stopGrace = 5 * time.Second

const stopPoll = 25 * time.Millisecond

// Stop ends the running session.
//
// Sessions run in the foreground, so this is a different process from the one
// holding the session. It needs no IPC channel: the record carries the owner's
// PID, and a signal is enough. The stopping process never writes to the record
// — the owner is the sole writer, which is what makes atomic writes sufficient.
func (s *Service) Stop(ctx context.Context) (*session.Session, error) {
	// If the lock is free, nothing is running, whatever the record says.
	acquired, lockErr := s.lock.TryAcquire()
	if lockErr == nil && acquired {
		defer s.releaseLock()

		if err := s.recoverStaleSession(ctx); err != nil {
			return nil, err
		}
		return nil, ErrNoSession
	}

	record, err := s.store.ReadSession()
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, ErrNoSession
	case err != nil:
		return nil, err
	case record.Status.IsTerminal():
		return nil, ErrNoSession
	}

	if record.OwnerPID <= 0 || !processAlive(record.OwnerPID) {
		return nil, ErrNoSession
	}

	// SIGTERM maps to user_stopped; the owner records the interpretation, and
	// the trace records the raw fact.
	if err := syscall.Kill(record.OwnerPID, syscall.SIGTERM); err != nil {
		return nil, err
	}

	return s.awaitEnding(ctx, record.ID)
}

// awaitEnding waits for the owner to write its terminal record.
//
// It never escalates to SIGKILL: escalation is a destructive act the user
// should choose, and the platform mechanism dies with its parent regardless
// (ADR-0006), so a hung Awake cannot leave the machine permanently awake.
func (s *Service) awaitEnding(ctx context.Context, id session.ID) (*session.Session, error) {
	deadline := time.Now().Add(stopGrace)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(stopPoll):
		}

		record, err := s.store.ReadSession()
		if err != nil {
			continue
		}
		if record.ID == id && record.Status.IsTerminal() {
			return record, nil
		}
	}

	return nil, ErrStopTimeout
}

// processAlive reports whether a PID is a live process. Signal 0 performs the
// error checking without sending anything.
//
// This is a diagnostic, not the exclusivity mechanism: ADR-0008 makes the lock
// authoritative precisely because a PID can be reused.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
