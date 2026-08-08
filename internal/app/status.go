package app

import (
	"context"
	"errors"
	"time"

	"github.com/javiervargas02/awake/internal/session"
	"github.com/javiervargas02/awake/internal/store"
)

// StatusResult describes the current or most recent session.
//
// Session is nil when nothing has ever run, which is information rather than
// an error: a status command that fails on a normal state is hostile to
// scripts.
type StatusResult struct {
	Session *session.Session
	Running bool

	// Remaining is nil for an indefinite session, which has no remaining time.
	// Callers render that as "no scheduled end" rather than as zero.
	Remaining *time.Duration
}

// Status reports on the session, performing stale detection first so that it
// can never describe a crashed session as running.
func (s *Service) Status(ctx context.Context) (*StatusResult, error) {
	if err := s.recoverIfStale(ctx); err != nil {
		return nil, err
	}

	record, err := s.store.ReadSession()
	switch {
	case errors.Is(err, store.ErrNotFound):
		return &StatusResult{}, nil
	case err != nil:
		return nil, err
	}

	result := &StatusResult{
		Session: record,
		Running: !record.Status.IsTerminal(),
	}

	if result.Running {
		if remaining, ok := record.Remaining(s.clock.Now()); ok {
			result.Remaining = &remaining
		}
	}

	return result, nil
}

// recoverIfStale resolves a crashed session without taking the lock for a
// session of our own. It is what lets every command, not just start, report
// the truth.
func (s *Service) recoverIfStale(ctx context.Context) error {
	acquired, err := s.lock.TryAcquire()
	if err != nil {
		// Cannot consult the lock; leave the record alone rather than
		// declaring a live session dead on a guess.
		return nil
	}
	if !acquired {
		// Someone genuinely holds it: the record is accurate.
		return nil
	}
	defer s.releaseLock()

	return s.recoverStaleSession(ctx)
}
