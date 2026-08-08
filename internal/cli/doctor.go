package cli

import (
	"context"
	"fmt"

	"github.com/javiervargas02/awake/internal/health"
)

// doctorOutput is the shape of `awake doctor --json`.
type doctorOutput struct {
	SchemaVersion int              `json:"schema_version"`
	Healthy       bool             `json:"healthy"`
	Summary       summary          `json:"summary"`
	Findings      []health.Finding `json:"findings"`
}

type summary struct {
	Total    int `json:"total"`
	OK       int `json:"ok"`
	Warnings int `json:"warnings"`
	Problems int `json:"problems"`
}

func runDoctor(ctx context.Context, args []string, deps Deps) error {
	var opts options

	fs := newFlagSet("doctor", &opts)
	operands, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(operands) > 0 {
		return usagef("doctor takes no arguments")
	}

	svc, cleanup, err := service(deps, opts.verbose)
	if err != nil {
		return err
	}
	defer cleanup()

	svc.Started("doctor")

	report := svc.Diagnose(ctx)

	if opts.json {
		if err := writeJSON(deps.Stdout, doctorOutput{
			SchemaVersion: 1,
			Healthy:       report.Healthy(),
			Summary: summary{
				Total: len(report.Findings), OK: report.OK(),
				Warnings: report.Warnings(), Problems: report.Problems(),
			},
			Findings: report.Findings,
		}); err != nil {
			return err
		}
	} else {
		writeFindings(deps, report)
	}

	// Warnings are informational: a machine with no config file and no history
	// is healthy, not broken. Only problems change the exit code, so a script
	// can tell the difference without parsing output.
	if !report.Healthy() {
		return &ProblemsFoundError{Count: report.Problems()}
	}
	return nil
}

func writeFindings(deps Deps, report health.Report) {
	for _, finding := range report.Findings {
		fmt.Fprintf(deps.Stdout, "%-8s %-30s %s\n",
			marker(finding.Status), finding.Check, finding.Detail)

		if finding.Remedy != "" && finding.Status != health.StatusOK {
			fmt.Fprintf(deps.Stdout, "%-8s %-30s %s\n", "", "", "→ "+finding.Remedy)
		}
	}

	fmt.Fprintf(deps.Stdout, "\n%d checks: %d ok, %d warning(s), %d problem(s)\n",
		len(report.Findings), report.OK(), report.Warnings(), report.Problems())

	switch {
	case !report.Healthy():
		fmt.Fprintln(deps.Stdout, "\nRun 'awake repair' to fix what can be fixed safely.")
	case report.Warnings() > 0:
		fmt.Fprintln(deps.Stdout, "\nNothing needs attention. Warnings are informational.")
	}
}

// marker labels a finding in words rather than symbols: this is a system tool,
// and its output should read like one.
func marker(status health.Status) string {
	switch status {
	case health.StatusProblem:
		return "PROBLEM"
	case health.StatusWarning:
		return "warning"
	default:
		return "ok"
	}
}
