package cli

import (
	"context"
	"fmt"
)

// stopOutput is the shape of `awake stop --json`.
type stopOutput struct {
	SchemaVersion int            `json:"schema_version"`
	Stopped       bool           `json:"stopped"`
	Session       *sessionOutput `json:"session"`
}

func runStop(ctx context.Context, args []string, deps Deps) error {
	var opts options

	fs := newFlagSet("stop", &opts)
	operands, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(operands) > 0 {
		return usagef("stop takes no arguments")
	}

	svc, cleanup, err := service(deps, opts.verbose)
	if err != nil {
		return err
	}
	defer cleanup()

	svc.Started("stop")

	final, err := svc.Stop(ctx)
	if err != nil {
		return err
	}

	now := *final.EndedAt

	if opts.json {
		return writeJSON(deps.Stdout, stopOutput{
			SchemaVersion: 1,
			Stopped:       true,
			Session:       describeSession(final, now),
		})
	}

	fmt.Fprintln(deps.Stdout, describeEnding(final, now))
	return nil
}
