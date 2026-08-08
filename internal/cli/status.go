package cli

import (
	"context"
	"fmt"
)

// statusOutput is the shape of `awake status --json`.
//
// This is the supported way for a script to read Awake's state. Session is
// null when nothing has ever run, which is information rather than an error.
type statusOutput struct {
	SchemaVersion int            `json:"schema_version"`
	Running       bool           `json:"running"`
	Session       *sessionOutput `json:"session"`
}

func runStatus(ctx context.Context, args []string, deps Deps) error {
	var opts options

	fs := newFlagSet("status", &opts)
	operands, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(operands) > 0 {
		return usagef("status takes no arguments")
	}

	svc, cleanup, err := service(deps, opts.verbose)
	if err != nil {
		return err
	}
	defer cleanup()

	svc.Started("status")

	// Status performs stale detection like every other command, so it can
	// never report a crashed session as running.
	result, err := svc.Status(ctx)
	if err != nil {
		return err
	}

	// The core's clock, not ours: a frontend must never have a second opinion
	// about what time it is.
	now := result.Now

	if opts.json {
		return writeJSON(deps.Stdout, statusOutput{
			SchemaVersion: 1,
			Running:       result.Running,
			Session:       describeSession(result.Session, now),
		})
	}

	switch {
	case result.Session == nil:
		fmt.Fprintln(deps.Stdout, "No sessions have run on this computer yet.")

	case result.Running:
		sess := result.Session
		fmt.Fprintf(deps.Stdout, "Keeping this computer awake.\n\n")
		fmt.Fprintf(deps.Stdout, "  session   %s\n", sess.ID)
		fmt.Fprintf(deps.Stdout, "  mode      %s\n", sess.Mode)
		fmt.Fprintf(deps.Stdout, "  started   %s\n", humanTime(sess.StartedAt))

		if result.Remaining != nil {
			fmt.Fprintf(deps.Stdout, "  ends      %s (%s from now)\n",
				humanTime(*sess.Deadline), humanDuration(*result.Remaining))
		} else {
			fmt.Fprintf(deps.Stdout, "  ends      no scheduled end\n")
		}

	default:
		sess := result.Session
		fmt.Fprintln(deps.Stdout, "No session is running.")
		fmt.Fprintf(deps.Stdout, "\nLast session %s\n", sess.ID)
		fmt.Fprintf(deps.Stdout, "  %s\n", describeEnding(sess, *sess.EndedAt))
		fmt.Fprintf(deps.Stdout, "  ended     %s (%s)\n", humanTime(*sess.EndedAt), sess.EndReason)
	}

	return nil
}
