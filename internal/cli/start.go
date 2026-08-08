package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/javiervargas02/awake/internal/app"
	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/session"
)

// startOutput is one line of `awake start --json`.
//
// Start emits JSON Lines rather than a single object: a long-running command
// that says nothing until it finishes is unusable in a script, so a caller can
// act on the session beginning without waiting for it to end.
type startOutput struct {
	SchemaVersion int            `json:"schema_version"`
	Event         string         `json:"event"` // "started" | "ended"
	Verification  string         `json:"verification,omitempty"`
	Mechanism     string         `json:"mechanism,omitempty"`
	Session       *sessionOutput `json:"session"`
}

func runStart(ctx context.Context, args []string, deps Deps) error {
	var opts options
	var indefinite bool

	fs := newFlagSet("start", &opts)
	fs.BoolVar(&indefinite, "indefinite", false, "run until stopped; never the default")
	operands, err := parseFlags(fs, args)
	if err != nil {
		return err
	}

	if len(operands) > 1 {
		return usagef("start takes at most one duration, got %d arguments", len(operands))
	}

	var duration time.Duration
	if len(operands) == 1 {
		if indefinite {
			return usagef("a duration cannot be combined with --indefinite")
		}

		parsed, parseErr := time.ParseDuration(operands[0])
		if parseErr != nil {
			return usagef("%q is not a duration; try 30m, 1h30m or 90s", operands[0])
		}
		if parsed <= 0 {
			return usagef("a duration must be positive, got %s", operands[0])
		}
		duration = parsed
	}

	svc, cleanup, err := service(deps, opts.verbose)
	if err != nil {
		return err
	}
	defer cleanup()

	svc.Started("start")

	running, err := svc.Start(ctx, app.StartRequest{
		Mode:       session.ModeSystem,
		Duration:   duration,
		Indefinite: indefinite,
	})
	if err != nil {
		return err
	}

	reportStarted(deps, opts, running)

	// The countdown is a terminal affordance, not content. When stdout is
	// piped it is omitted entirely rather than repeated into a file.
	var countdownDone chan struct{}
	if deps.Interactive && !opts.json {
		countdownDone = startCountdown(deps.Stdout, running.Session)
	}

	final, err := running.Wait(ctx)
	if countdownDone != nil {
		close(countdownDone)
		clearLine(deps.Stdout)
	}
	if err != nil {
		return err
	}

	return reportEnded(deps, opts, running, final)
}

func reportStarted(deps Deps, opts options, running *app.RunningSession) {
	sess := running.Session
	now := sess.StartedAt

	if opts.json {
		_ = writeJSON(deps.Stdout, startOutput{
			SchemaVersion: 1,
			Event:         "started",
			Verification:  string(running.Verification),
			Mechanism:     running.Mechanism,
			Session:       describeSession(sess, now),
		})
	} else if sess.Indefinite {
		fmt.Fprintf(deps.Stdout,
			"Keeping this computer awake — %s mode, no scheduled end.\nPress Ctrl-C to stop, or run 'awake stop'.\n",
			sess.Mode)
	} else {
		remaining, _ := sess.Remaining(now)
		fmt.Fprintf(deps.Stdout,
			"Keeping this computer awake — %s mode, until %s (%s from now).\nPress Ctrl-C to stop, or run 'awake stop'.\n",
			sess.Mode, humanTime(*sess.Deadline), humanDuration(remaining))
	}

	// A warning is not the command's result, so it goes to stderr even in
	// human mode. Saying "running, but I could not confirm it" is honest;
	// saying nothing would not be.
	if running.Verification == platform.Unverifiable {
		fmt.Fprintln(deps.Stderr,
			"awake: could not confirm the sleep assertion with the system; the session is running unverified.")
	}
}

func reportEnded(deps Deps, opts options, running *app.RunningSession, final *session.Session) error {
	now := final.StartedAt
	if final.EndedAt != nil {
		now = *final.EndedAt
	}

	if opts.json {
		return writeJSON(deps.Stdout, startOutput{
			SchemaVersion: 1,
			Event:         "ended",
			Mechanism:     running.Mechanism,
			Session:       describeSession(final, now),
		})
	}

	fmt.Fprintln(deps.Stdout, describeEnding(final, now))

	// A failed session is reported as an error so the exit code matches what
	// the user was told.
	if final.Status == session.StatusFailed {
		return fmt.Errorf("session ended in failure: %s", final.EndReason)
	}
	return nil
}

// startCountdown shows the time remaining on a single rewritten line.
func startCountdown(out io.Writer, sess *session.Session) chan struct{} {
	done := make(chan struct{})

	if sess.Indefinite {
		return done
	}

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				remaining, ok := sess.Remaining(time.Now().UTC())
				if !ok {
					return
				}
				fmt.Fprintf(out, "\r  %s remaining   ", remaining.Round(time.Second))
			}
		}
	}()

	return done
}

func clearLine(out io.Writer) {
	fmt.Fprint(out, "\r\033[K")
}
