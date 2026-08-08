// Package app is Awake's application core.
//
// It holds the use cases — start a session, stop the running one, report
// status — and owns the orchestration between the session domain, the store,
// the logger, the exclusivity lock and the platform. The CLI is a thin client
// over this package (ADR-0001); any future GUI or local API is a peer of the
// CLI, not a wrapper around it.
//
// Nothing here formats output for a terminal, prints, or knows what an exit
// code is. Operations return structured results and typed errors.
package app

import (
	"errors"
	"os"

	"github.com/javiervargas02/awake/internal/clock"
	"github.com/javiervargas02/awake/internal/config"
	"github.com/javiervargas02/awake/internal/lock"
	"github.com/javiervargas02/awake/internal/logging"
	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/store"
)

var (
	// ErrSessionRunning: a session already holds the machine.
	ErrSessionRunning = errors.New("a session is already running")

	// ErrNoSession: there is nothing to stop.
	ErrNoSession = errors.New("no session is running")

	// ErrStopTimeout: the session did not end within the grace period.
	ErrStopTimeout = errors.New("session did not end in time")

	// ErrStopRequested and ErrInterrupted are cancellation *causes*. The
	// composition root attaches one when it cancels a session's context, and
	// the core reads it to decide the end reason — so the core learns "stopped
	// by the user" or "interrupted" and never learns that signals exist.
	ErrStopRequested = errors.New("stop requested")
	ErrInterrupted   = errors.New("interrupted")
)

// Deps are the concrete pieces the service works with. They are injected so
// that the composition root is the only place that knows which
// implementations are in play, and so tests can supply fakes.
type Deps struct {
	Clock      clock.Clock
	Store      *store.Store
	Logger     *logging.Logger
	Platform   platform.Controller
	Lock       lock.Guard
	AppVersion string
}

// Service is the application core.
type Service struct {
	clock      clock.Clock
	store      *store.Store
	logger     *logging.Logger
	platform   platform.Controller
	lock       lock.Guard
	appVersion string
}

func New(deps Deps) *Service {
	c := deps.Clock
	if c == nil {
		c = clock.System{}
	}
	return &Service{
		clock:      c,
		store:      deps.Store,
		logger:     deps.Logger,
		platform:   deps.Platform,
		lock:       deps.Lock,
		appVersion: deps.AppVersion,
	}
}

// loadConfig reads configuration and reports what happened, so that every
// operation logs the same facts without the CLI having to re-derive them.
//
// A bad config never prevents an operation: it degrades per key and says so.
func (s *Service) loadConfig() config.Config {
	cfg, report, err := config.Load(s.store.ConfigPath())

	s.logger.Info(logging.EventConfigLoaded, logging.Fields{
		"source": string(report.Source),
	})

	for _, defaulted := range report.Defaulted {
		s.logger.Warn(logging.EventConfigDefaulted, logging.Fields{
			"key":    defaulted.Key,
			"reason": string(defaulted.Reason),
		})
	}
	for _, key := range report.UnknownKeys {
		s.logger.Warn(logging.EventConfigUnknownKey, logging.Fields{"key": key})
	}
	for _, implausible := range report.Implausible {
		// A value that parses but looks unintended is honoured — it is the
		// user's machine — and reported as a defaulted-style warning so that
		// doctor and the logs both surface it.
		s.logger.Warn(logging.EventConfigDefaulted, logging.Fields{
			"key":    implausible.Key,
			"reason": string(config.ReasonInvalidValue),
		})
	}

	_ = err // the config is usable regardless; the report carries the detail
	return cfg
}

// ownerPID reports the process that owns a session. It is a diagnostic
// (ADR-0008): the lock decides liveness, this explains what happened.
func ownerPID() int { return os.Getpid() }
