package cli

import (
	"context"
	"fmt"

	"github.com/javiervargas02/awake/internal/health"
)

// repairOutput is the shape of `awake repair --json`.
type repairOutput struct {
	SchemaVersion int             `json:"schema_version"`
	Applied       []repairApplied `json:"applied"`
	Healthy       bool            `json:"healthy_before"`
}

type repairApplied struct {
	Action  string `json:"action"`
	Target  string `json:"target"`
	Outcome string `json:"outcome"`
	Failed  bool   `json:"failed"`
}

func runRepair(ctx context.Context, args []string, deps Deps) error {
	var opts options
	var cleanQuarantine bool

	fs := newFlagSet("repair", &opts)
	fs.BoolVar(&cleanQuarantine, "clean-quarantine", false,
		"also delete files Awake set aside after finding them corrupt")

	operands, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(operands) > 0 {
		return usagef("repair takes no arguments")
	}

	svc, cleanup, err := service(deps, opts.verbose)
	if err != nil {
		return err
	}
	defer cleanup()

	svc.Started("repair")

	result, err := svc.Repair(ctx, cleanQuarantine)
	if err != nil {
		return err
	}

	if opts.json {
		applied := make([]repairApplied, 0, len(result.Applied))
		for _, r := range result.Applied {
			applied = append(applied, repairApplied{
				Action: string(r.Action), Target: r.Target,
				Outcome: r.Outcome, Failed: r.Err != nil,
			})
		}
		if err := writeJSON(deps.Stdout, repairOutput{
			SchemaVersion: 1,
			Applied:       applied,
			Healthy:       result.Report.Healthy(),
		}); err != nil {
			return err
		}
	} else {
		writeRepairs(deps, result.Applied, cleanQuarantine, result.Report)
	}

	for _, r := range result.Applied {
		if r.Err != nil {
			return fmt.Errorf("one or more repairs failed; run 'awake doctor' for the current state")
		}
	}
	return nil
}

func writeRepairs(deps Deps, applied []health.Result, cleanQuarantine bool, report health.Report) {
	if len(applied) == 0 {
		// On a healthy installation repair does nothing and says so, rather
		// than being silent.
		fmt.Fprintln(deps.Stdout, "Nothing to repair.")
	} else {
		for _, r := range applied {
			fmt.Fprintf(deps.Stdout, "  %s\n", r.Outcome)
		}
		fmt.Fprintf(deps.Stdout, "\n%d repair(s) applied.\n", len(applied))
	}

	// Name the flag rather than acting: deleting a user's files is their
	// decision, and an explicit flag is what makes the choice theirs.
	//
	// This is reported whether or not anything else was repaired. Set-aside
	// files are usually the only thing left, and a user who is told "nothing
	// to repair" would otherwise never learn the flag exists.
	if cleanQuarantine {
		return
	}
	for _, finding := range report.Actionable() {
		if finding.Action != health.ActionCleanQuarantine {
			continue
		}
		fmt.Fprintln(deps.Stdout,
			"\nFiles that were found corrupt have been set aside, not deleted.\n"+
				"Inspect them, then run 'awake repair --clean-quarantine' to remove them.")
		return
	}
}
