package app

import (
	"context"

	"github.com/javiervargas02/awake/internal/health"
	"github.com/javiervargas02/awake/internal/logging"
)

// Diagnose inspects the installation without changing anything.
func (s *Service) Diagnose(ctx context.Context) health.Report {
	doctor := health.NewDoctor(s.store, s.platform, s.lock, s.clock)
	report := doctor.Diagnose(ctx)

	level := logging.LevelInfo
	if !report.Healthy() {
		level = logging.LevelWarn
	}

	// findings carries every check's outcome, so a log reader can see what was
	// inspected rather than only how many things were wrong.
	findings := make([]map[string]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		findings = append(findings, map[string]string{
			"check":  finding.Check,
			"status": finding.Status.String(),
		})
	}

	s.logger.Log(level, logging.EventHealthCheckCompleted, logging.Fields{
		"total":    len(report.Findings),
		"ok":       report.OK(),
		"warnings": report.Warnings(),
		"problems": report.Problems(),
		"findings": findings,
	})

	return report
}

// RepairResult is what a repair run did.
type RepairResult struct {
	Report  health.Report
	Applied []health.Result
}

// Repair diagnoses, then applies the safe fixes that diagnosis called for.
//
// Running doctor first is not an optimisation — it is the guarantee: repair
// acts only on findings, so it can never do something doctor would not have
// predicted.
func (s *Service) Repair(ctx context.Context, cleanQuarantine bool) (*RepairResult, error) {
	report := s.Diagnose(ctx)

	repairer := health.NewRepairer(s.store, s.clock)
	repairer.CleanQuarantine = cleanQuarantine

	applied := repairer.Apply(ctx, report)

	for _, result := range applied {
		// Repairs log at warn because a repair means something was wrong:
		// silent self-repair is indistinguishable from a bug.
		s.logger.Warn(logging.EventRepairPerformed, logging.Fields{
			"action": string(result.Action),
			"target": s.relativeToRoot(result.Target),
			"result": result.Outcome,
		})
	}

	// Recovery of a crashed session belongs to the session lifecycle, not to
	// repair, so it runs here through the same path every command uses.
	if err := s.recoverIfStale(ctx); err != nil {
		return nil, err
	}

	return &RepairResult{Report: report, Applied: applied}, nil
}

// relativeToRoot keeps logged paths inside Awake's own directory, so a trace
// never carries more of the filesystem than it needs to.
func (s *Service) relativeToRoot(path string) string {
	root := s.store.Root()
	if len(path) > len(root) && path[:len(root)] == root {
		return "~/.awake" + path[len(root):]
	}
	if path == root {
		return "~/.awake"
	}
	return path
}
