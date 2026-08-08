// Package cli is the terminal frontend for Awake.
//
// It is a thin client (ADR-0001): it parses input into a typed request, calls
// one core operation, and renders the result. It holds no logic that a future
// GUI or local API would have to reimplement, and it is the only layer that
// knows exit codes and human phrasing exist.
//
// One rule shapes the output: stdout is a data channel. Anything that is not
// the command's result — warnings, progress, echoed events — goes to stderr,
// so that piping a command never yields something a program has to clean up.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/javiervargas02/awake/internal/app"
	"github.com/javiervargas02/awake/internal/buildinfo"
)

// Deps is everything the CLI needs from the composition root.
//
// The service arrives as a factory rather than a value so that commands which
// touch no state — version, help — cause nothing to be created on disk.
// Installing Awake and asking its version leaves no trace.
type Deps struct {
	Stdout io.Writer
	Stderr io.Writer

	// Interactive reports whether stdout is a terminal. It controls the live
	// countdown and nothing else: piped output must not gain or lose content.
	Interactive bool

	Version buildinfo.Info

	// NewService builds the application core and returns a cleanup function.
	NewService func(verbose bool) (*app.Service, func(), error)
}

// options holds the flags accepted by every command.
type options struct {
	json    bool
	verbose bool
}

type command struct {
	name    string
	summary string
	usage   string
	run     func(ctx context.Context, args []string, deps Deps) error
}

func commands() []command {
	return []command{
		{
			name:    "start",
			summary: "Start a session that keeps this computer awake",
			usage:   "awake start [duration] [--indefinite]",
			run:     runStart,
		},
		{
			name:    "stop",
			summary: "End the running session",
			usage:   "awake stop",
			run:     runStop,
		},
		{
			name:    "status",
			summary: "Show the current or most recent session",
			usage:   "awake status",
			run:     runStatus,
		},
		{
			name:    "update",
			summary: "Check whether a newer release exists (never installs)",
			usage:   "awake update check [--force]",
			run:     runUpdate,
		},
		{
			name:    "doctor",
			summary: "Check this installation and explain anything wrong",
			usage:   "awake doctor",
			run:     runDoctor,
		},
		{
			name:    "repair",
			summary: "Apply the safe fixes doctor identified",
			usage:   "awake repair [--clean-quarantine]",
			run:     runRepair,
		},
		{
			name:    "version",
			summary: "Print version and build information",
			usage:   "awake version",
			run:     runVersion,
		},
	}
}

// Run executes a single CLI invocation and returns the process exit code.
func Run(ctx context.Context, args []string, deps Deps) int {
	err := dispatch(ctx, args, deps)
	if err == nil {
		return ExitOK
	}

	code := exitCodeFor(err)

	var usage *UsageError
	if errors.As(err, &usage) {
		fmt.Fprintf(deps.Stderr, "awake: %v\n\nRun 'awake --help' for usage.\n", err)
	} else {
		fmt.Fprintf(deps.Stderr, "awake: %v\n", err)
	}
	return code
}

func dispatch(ctx context.Context, args []string, deps Deps) error {
	// Bare `awake` prints help and succeeds: asking a tool what it does is not
	// a mistake.
	if len(args) == 0 {
		writeHelp(deps.Stdout)
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		writeHelp(deps.Stdout)
		return nil
	}

	if strings.HasPrefix(args[0], "-") {
		return usagef("unknown flag %q; global flags follow the command name", args[0])
	}

	for _, cmd := range commands() {
		if cmd.name == args[0] {
			return cmd.run(ctx, args[1:], deps)
		}
	}

	return usagef("unknown command %q", args[0])
}

// newFlagSet builds a flag set that already carries the global flags, so that
// every command accepts them identically.
//
// Parse errors are returned rather than printed-and-exited, which is the
// standard library's default: the caller decides the exit code.
func newFlagSet(name string, opts *options) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.json, "json", false, "emit machine-readable JSON on stdout")
	fs.BoolVar(&opts.verbose, "verbose", false, "mirror log events to stderr")
	return fs
}

// parseFlags parses a command's arguments and returns its positional operands.
//
// The standard library stops parsing at the first non-flag argument, which
// would make `awake start 30m --json` a usage error while `awake start --json
// 30m` worked. Users write the first form, and a tool that rejects it is
// failing them over an implementation detail. Parsing in a loop — take one
// operand, resume parsing — accepts flags and operands in any order.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var operands []string

	for {
		if err := fs.Parse(args); err != nil {
			return nil, &UsageError{Err: err}
		}

		rest := fs.Args()
		if len(rest) == 0 {
			return operands, nil
		}

		operands = append(operands, rest[0])
		args = rest[1:]
	}
}

// service builds the application core, converting a wiring failure into a
// plain error rather than a panic.
func service(deps Deps, verbose bool) (*app.Service, func(), error) {
	if deps.NewService == nil {
		return nil, nil, errors.New("no application service is configured")
	}
	return deps.NewService(verbose)
}

func writeHelp(out io.Writer) {
	fmt.Fprint(out, `awake - keep this computer awake for a bounded, observable session

Usage:
  awake <command> [flags]

Commands:
`)
	for _, cmd := range commands() {
		fmt.Fprintf(out, "  %-9s %s\n", cmd.name, cmd.summary)
	}
	fmt.Fprint(out, `
Global flags (written after the command name):
  --json      emit machine-readable JSON on stdout
  --verbose   mirror log events to stderr
  -h, --help  show this help

Examples:
  awake start            start a session of the default length
  awake start 90m        keep this computer awake for 90 minutes
  awake start --indefinite
                         run until stopped; never the default
  awake status --json    machine-readable state, for scripts
  awake doctor           check whether anything is wrong
  awake update check     see whether a newer release exists

Exit codes:
  0  success
  1  unexpected internal error
  2  usage error
  3  precondition not met (a session is already running, or none is)
  5  diagnostics found problems

Sessions are bounded, logged under ~/.awake/logs, and end on their own.
Awake is not a stealth tool: it does not hide its activity.
`)
}
