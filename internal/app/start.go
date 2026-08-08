package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/javiervargas02/awake/internal/logging"
	"github.com/javiervargas02/awake/internal/mode"
	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/session"
	"github.com/javiervargas02/awake/internal/store"
)

// StartRequest describes a session the user asked for.
type StartRequest struct {
	Mode       session.Mode
	Duration   time.Duration
	Indefinite bool
}

// RunningSession is a started session, returned as soon as the machine is
// actually being kept awake.
//
// Start returns rather than blocking so that a caller can report the session
// began — and a script can act on it — before waiting for it to end.
type RunningSession struct {
	Session      *session.Session
	Verification platform.Verification
	Mechanism    string

	service *Service
	logger  *logging.Logger
	handle  platform.Handle
	trace   io.Closer
	done    chan struct{}
	final   *session.Session
}

// Start begins a session.
//
// The order of operations is deliberate and is the reason a crash is always
// attributable: the lock is taken first, the record is written before the
// platform is touched, and the mechanism is verified before the session is
// reported as running.
func (s *Service) Start(ctx context.Context, req StartRequest) (*RunningSession, error) {
	cfg := s.loadConfig()

	if req.Mode == "" {
		req.Mode = session.ModeSystem
	}
	if !req.Indefinite && req.Duration == 0 {
		req.Duration = cfg.Session.DefaultDuration
	}

	runner, err := mode.For(req.Mode, s.platform)
	if err != nil {
		return nil, err
	}

	// 1. Exclusivity, atomically. Nothing else can be true until this is.
	degradedLock, err := s.acquireLock()
	if err != nil {
		return nil, err
	}

	// 2. A non-terminal record with the lock free means a previous run died.
	if err := s.recoverStaleSession(ctx); err != nil {
		s.releaseLock()
		return nil, err
	}

	now := s.clock.Now()
	sess, err := session.New(now, session.Params{
		Mode:       req.Mode,
		Duration:   req.Duration,
		Indefinite: req.Indefinite,
		AppVersion: s.appVersion,
		OwnerPID:   ownerPID(),
	})
	if err != nil {
		s.releaseLock()
		return nil, err
	}

	// 3. Bind a logger to the session so every event lands in its own trace as
	// well as the global log, without any caller remembering to attach an ID.
	sessionLogger, trace := s.openTrace(sess.ID)

	sessionLogger.Info(logging.EventSessionCreated, logging.Fields{
		"app_version":        sess.AppVersion,
		"mode":               sess.Mode.String(),
		"requested_duration": sess.RequestedDuration.String(),
		"indefinite":         sess.Indefinite,
		"deadline":           formatDeadline(sess),
		"owner_pid":          sess.OwnerPID,
	})
	if degradedLock {
		sessionLogger.Warn(logging.EventSessionStartRefused, logging.Fields{
			"reason": "lock_unavailable",
		})
	}

	// 4. Persist before touching the platform, so a crash in the next step is
	// always attributable to a session that exists on disk.
	if err := s.store.WriteSession(sess); err != nil {
		closeTrace(trace)
		s.releaseLock()
		return nil, fmt.Errorf("recording session: %w", err)
	}

	// 5. Start the mechanism and confirm the machine is really being held awake.
	handle, err := runner.Start(ctx)
	if err != nil {
		sessionLogger.Error(logging.EventModeFailed, logging.Fields{
			"mode":  sess.Mode.String(),
			"error": err.Error(),
		})
		s.endSession(sessionLogger, sess, s.clock.Now(), session.StatusFailed, session.ReasonModeFailure, err)
		closeTrace(trace)
		s.releaseLock()
		return nil, err
	}

	sess.MechanismPID = handle.PID()
	if writeErr := s.store.WriteSession(sess); writeErr != nil {
		// The mechanism is running; losing the record would make it
		// unattributable, which is exactly what layer 3 needs to avoid.
		_ = handle.Stop()
		closeTrace(trace)
		s.releaseLock()
		return nil, fmt.Errorf("recording mechanism: %w", writeErr)
	}

	level := logging.LevelInfo
	if handle.Verification() == platform.Unverifiable {
		level = logging.LevelWarn
	}
	sessionLogger.Log(level, logging.EventModeStarted, logging.Fields{
		"mode":               sess.Mode.String(),
		"mechanism":          runner.Mechanism(),
		"mechanism_pid":      handle.PID(),
		"assertion_verified": string(handle.Verification()),
	})

	sessionLogger.Info(logging.EventSessionStarted, nil)

	return &RunningSession{
		Session:      sess,
		Verification: handle.Verification(),
		Mechanism:    runner.Mechanism(),
		service:      s,
		logger:       sessionLogger,
		handle:       handle,
		trace:        trace,
		done:         make(chan struct{}),
	}, nil
}

// Wait blocks until the session ends and returns the final record.
//
// Three things can end a session, and all three run the same shutdown path:
// the deadline arrives, the mechanism dies, or the context is cancelled. One
// shutdown path with different reasons is what stops an ending from being
// half-performed depending on how it was triggered.
func (r *RunningSession) Wait(ctx context.Context) (*session.Session, error) {
	select {
	case <-r.done:
		return r.final, nil
	default:
	}
	defer close(r.done)

	s := r.service
	sess := r.Session

	var deadline <-chan time.Time
	if !sess.Indefinite {
		remaining, _ := sess.Remaining(s.clock.Now())
		deadline = s.clock.After(remaining)
	}

	var (
		status session.Status
		reason session.EndReason
		cause  error
	)

	select {
	case <-deadline:
		status, reason = session.StatusCompleted, session.ReasonDurationElapsed

	case err := <-r.handle.Died():
		status, reason, cause = session.StatusFailed, session.ReasonModeFailure, err
		r.logger.Error(logging.EventModeFailed, logging.Fields{
			"mode":  sess.Mode.String(),
			"error": err.Error(),
		})

	case <-ctx.Done():
		status = session.StatusStopped
		reason = session.ReasonInterrupted
		if errors.Is(context.Cause(ctx), ErrStopRequested) {
			reason = session.ReasonUserStopped
		}
	}

	now := s.clock.Now()

	// The machine may have slept through the deadline. The stored instant
	// decides, not the timer that woke us.
	if status != session.StatusFailed && sess.DeadlinePassed(now) {
		status, reason = session.StatusCompleted, session.ReasonDurationElapsed
	}

	if err := r.handle.Stop(); err != nil {
		r.logger.Error(logging.EventModeFailed, logging.Fields{
			"mode":  sess.Mode.String(),
			"error": err.Error(),
		})
	} else {
		r.logger.Info(logging.EventModeStopped, logging.Fields{"mode": sess.Mode.String()})
	}

	s.endSession(r.logger, sess, now, status, reason, cause)

	closeTrace(r.trace)
	s.releaseLock()

	r.final = sess
	return sess, nil
}

// endSession applies the terminal transition, persists it, and logs it. It is
// the single shutdown path shared by every way a session can end.
func (s *Service) endSession(
	logger *logging.Logger,
	sess *session.Session,
	now time.Time,
	status session.Status,
	reason session.EndReason,
	cause error,
) {
	var err error
	switch status {
	case session.StatusCompleted:
		err = sess.Complete(now)
	case session.StatusStopped:
		err = sess.Stop(now, reason)
	case session.StatusFailed:
		err = sess.Fail(now, reason)
	}
	if err != nil {
		// The session already ended. Nothing to do but keep the first ending,
		// which is the one that actually happened.
		return
	}

	if writeErr := s.store.WriteSession(sess); writeErr != nil {
		logger.Warn(logging.EventLogSinkFailed, logging.Fields{
			"sink":  "session_record",
			"error": writeErr.Error(),
		})
	}

	fields := logging.Fields{
		"end_reason": sess.EndReason.String(),
		"elapsed":    sess.Elapsed(now).String(),
	}

	switch sess.Status {
	case session.StatusCompleted:
		fields["overrun"] = sess.Overrun(now).String()
		logger.Info(logging.EventSessionCompleted, fields)
	case session.StatusStopped:
		if remaining, ok := sess.Remaining(now); ok {
			fields["remaining"] = remaining.String()
		}
		logger.Info(logging.EventSessionStopped, fields)
	case session.StatusFailed:
		if cause != nil {
			fields["error"] = cause.Error()
		}
		logger.Error(logging.EventSessionFailed, fields)
	}
}

// acquireLock takes exclusivity, reporting whether it had to degrade.
func (s *Service) acquireLock() (degraded bool, err error) {
	acquired, lockErr := s.lock.TryAcquire()
	switch {
	case lockErr != nil:
		// Could not even ask. Fall back to record-based checking rather than
		// refusing to start: a working tool with a stated weakness beats a
		// tool that will not start because of a temp directory.
		if running, checkErr := s.recordSuggestsRunning(); checkErr == nil && running {
			s.logger.Warn(logging.EventSessionStartRefused, logging.Fields{
				"reason": "already_running",
			})
			return false, ErrSessionRunning
		}
		return true, nil

	case !acquired:
		s.logger.Warn(logging.EventSessionStartRefused, logging.Fields{
			"reason": "already_running",
		})
		return false, ErrSessionRunning
	}
	return false, nil
}

func (s *Service) releaseLock() {
	_ = s.lock.Release()
}

// recordSuggestsRunning is the degraded liveness check, used only when the
// lock could not be consulted.
func (s *Service) recordSuggestsRunning() (bool, error) {
	record, err := s.store.ReadSession()
	if err != nil {
		return false, err
	}
	return !record.Status.IsTerminal() && processAlive(record.OwnerPID), nil
}

func (s *Service) openTrace(id session.ID) (*logging.Logger, io.Closer) {
	file, err := logging.OpenFile(s.store.SessionLogPath(id.String()))
	if err != nil {
		// The session runs on with global logging only: logging must never be
		// the reason a session does not happen.
		s.logger.Warn(logging.EventLogSinkFailed, logging.Fields{
			"sink":  "session_trace",
			"error": err.Error(),
		})
		return s.logger.WithSession(id.String(), nil), nil
	}
	return s.logger.WithSession(id.String(), file), file
}

func closeTrace(trace io.Closer) {
	if trace != nil {
		_ = trace.Close()
	}
}

func formatDeadline(sess *session.Session) any {
	if sess.Deadline == nil {
		return nil
	}
	return sess.Deadline.Format(time.RFC3339Nano)
}

// recoverStaleSession resolves a session whose owner vanished.
//
// It runs before every start, and the end timestamp is the moment of
// discovery, not of death: Awake does not know when the process died and must
// not invent it.
func (s *Service) recoverStaleSession(ctx context.Context) error {
	record, err := s.store.ReadSession()
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case errors.Is(err, store.ErrCorrupt):
		// A broken record is a problem for doctor and repair, not something a
		// start should silently rewrite.
		return nil
	case err != nil:
		return err
	case record.Status.IsTerminal():
		return nil
	}

	now := s.clock.Now()
	reclaimed, reclaimErr := s.platform.Reclaim(ctx, record.MechanismPID)

	logger := s.logger.WithSession(record.ID.String(), nil)
	if file, openErr := logging.OpenFile(s.store.SessionLogPath(record.ID.String())); openErr == nil {
		logger = s.logger.WithSession(record.ID.String(), file)
		defer file.Close()
	}

	fields := logging.Fields{
		"owner_pid":          record.OwnerPID,
		"discovered_at":      now.Format(time.RFC3339Nano),
		"platform_reclaimed": reclaimed,
	}
	if reclaimErr != nil {
		fields["platform_reclaimed"] = false
	}
	logger.Warn(logging.EventSessionRecovered, fields)

	s.endSession(logger, record, now, session.StatusFailed, session.ReasonCrashed, reclaimErr)
	return nil
}
