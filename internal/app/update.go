package app

import (
	"context"
	"errors"

	"github.com/javiervargas02/awake/internal/logging"
	"github.com/javiervargas02/awake/internal/store"
	"github.com/javiervargas02/awake/internal/update"
)

// CheckUpdate reports whether a newer release exists.
//
// It never returns an error for a failed check: being offline is not a defect
// in Awake (ADR-0005), so the outcome carries that information instead.
func (s *Service) CheckUpdate(ctx context.Context, force bool) (update.Result, error) {
	cfg := s.loadConfig()
	now := s.clock.Now()

	// Disabled means no request of any kind. This is what makes principle 5's
	// "the only network activity" claim verifiable rather than aspirational.
	if !cfg.Updates.Enabled {
		return update.Result{
			Outcome:        update.OutcomeDisabled,
			Channel:        update.DefaultChannel,
			CurrentVersion: s.appVersion,
			CheckedAt:      now,
		}, nil
	}

	cached, haveCache := s.readUpdateCache()
	if !force && haveCache && cached.Fresh(now, cfg.Updates.CheckInterval) {
		return cached.AsResult(s.appVersion), nil
	}

	s.logger.Info(logging.EventUpdateCheckStarted, logging.Fields{
		"channel": update.DefaultChannel,
	})

	checker := s.updateChecker()
	result := checker.Check(ctx, s.appVersion, update.DefaultChannel, now)

	fields := logging.Fields{"result": result.Outcome.String()}
	if result.LatestVersion != "" {
		fields["latest_version"] = result.LatestVersion
	}
	if result.Err != nil {
		fields["error"] = result.Err.Error()
	}

	level := logging.LevelInfo
	if result.Outcome == update.OutcomeFailed {
		// A failed check is a warning, never an error.
		level = logging.LevelWarn
	}
	s.logger.Log(level, logging.EventUpdateCheckCompleted, fields)

	if result.Available() {
		s.logger.Info(logging.EventUpdateAvailable, logging.Fields{
			"current_version": result.CurrentVersion,
			"latest_version":  result.LatestVersion,
			"severity":        result.Severity.String(),
		})
	}

	// Cache failures too, so an offline machine does not retry on every
	// command.
	if err := s.store.WriteJSON(s.store.UpdatePath(), update.NewCache(result)); err != nil {
		s.logger.Warn(logging.EventLogSinkFailed, logging.Fields{
			"sink":  "update_cache",
			"error": err.Error(),
		})
	}

	return result, nil
}

// LastUpdateCheck returns the cached answer, if any, so that doctor can report
// a stale check as stale rather than implying freshness.
func (s *Service) LastUpdateCheck() (update.Cache, bool) {
	return s.readUpdateCache()
}

func (s *Service) readUpdateCache() (update.Cache, bool) {
	var cached update.Cache
	if err := s.store.ReadJSON(s.store.UpdatePath(), &cached); err != nil {
		if !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrCorrupt) {
			return update.Cache{}, false
		}
		return update.Cache{}, false
	}
	return cached, true
}

func (s *Service) updateChecker() *update.Checker {
	if s.checker != nil {
		return s.checker
	}
	return update.NewChecker()
}
