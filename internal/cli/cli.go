// Package cli is the terminal frontend for Awake.
//
// It is a thin client (ADR-0001): it parses input into a typed request, calls
// one core operation, and renders the result. It holds no logic that a future
// GUI or local API would have to reimplement, and it is the only layer that
// knows exit codes and human phrasing exist.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// options holds the flags accepted by every command.
type options struct {
	json    bool
	verbose bool
}

// command is one entry in the CLI's dispatch table.
type command struct {
	name    string
	summary string
	run     func(args []string, out io.Writer, errOut io.Writer) error
}

func commands() []command {
	return []command{
		{name: "version", summary: "Print version and build information", run: runVersion},
	}
}

// Run executes a single CLI invocation and returns the process exit code.
//
// It takes its output streams as parameters rather than writing to os.Stdout
// directly, so that tests can capture output without touching the real
// process. Everything user-facing flows through these two writers.
func Run(args []string, out, errOut io.Writer) int {
	err := dispatch(args, out, errOut)
	if err == nil {
		return ExitOK
	}

	code := exitCodeFor(err)

	// A usage error prints the mistake and points at help; anything else is a
	// plain failure message. Neither goes to stdout: stdout is a data channel.
	var usage *UsageError
	if errors.As(err, &usage) {
		fmt.Fprintf(errOut, "awake: %v\n\nRun 'awake --help' for usage.\n", err)
	} else {
		fmt.Fprintf(errOut, "awake: %v\n", err)
	}
	return code
}

func dispatch(args []string, out, errOut io.Writer) error {
	// Bare `awake` prints help and succeeds: asking a tool what it does is not
	// a mistake.
	if len(args) == 0 {
		writeHelp(out)
		return nil
	}

	switch args[0] {
	case "-h", "--help", "help":
		writeHelp(out)
		return nil
	}

	if strings.HasPrefix(args[0], "-") {
		return usagef("unknown flag %q; global flags follow the command name", args[0])
	}

	for _, cmd := range commands() {
		if cmd.name == args[0] {
			return cmd.run(args[1:], out, errOut)
		}
	}

	return usagef("unknown command %q", args[0])
}

// newFlagSet builds a flag set that already carries the global flags, so that
// every command accepts them identically.
//
// Errors are returned rather than printed-and-exited, which is the standard
// library's default behaviour; we want the caller to decide the exit code.
func newFlagSet(name string, opts *options) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.json, "json", false, "emit machine-readable JSON on stdout")
	fs.BoolVar(&opts.verbose, "verbose", false, "mirror log events to stderr")
	return fs
}

// parseFlags parses a command's arguments, converting any parse failure into a
// usage error so it maps to exit code 2.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return &UsageError{Err: err}
	}
	return nil
}

func writeHelp(out io.Writer) {
	fmt.Fprint(out, `awake - keep this computer awake for a bounded, observable session

Usage:
  awake <command> [flags]

Commands:
`)
	for _, cmd := range commands() {
		fmt.Fprintf(out, "  %-10s %s\n", cmd.name, cmd.summary)
	}
	fmt.Fprint(out, `
Global flags (accepted by every command):
  --json      emit machine-readable JSON on stdout
  --verbose   mirror log events to stderr
  -h, --help  show this help

Exit codes:
  0  success
  1  unexpected internal error
  2  usage error
  3  precondition not met
  5  diagnostics found problems

Awake is not a stealth tool. It does not hide its activity, and every
session leaves an audit trail under ~/.awake/logs.
`)
}
