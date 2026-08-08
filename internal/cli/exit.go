package cli

import (
	"errors"
	"fmt"

	"github.com/javiervargas02/awake/internal/app"
	"github.com/javiervargas02/awake/internal/session"
)

// Exit codes are public API (principle 8). The full table lives in
// docs/architecture/cli-contract.md; this is its single implementation.
//
// Codes 6-125 are unassigned. 126, 127 and 128+n are left to the shell.
const (
	ExitOK            = 0 // success
	ExitInternal      = 1 // unexpected internal error
	ExitUsage         = 2 // bad flags or arguments
	ExitPrecondition  = 3 // session already running, or none to stop
	ExitUpdateBlocked = 4 // RESERVED: blocked by a required security update (v0.2+)
	ExitProblemsFound = 5 // doctor found problems
)

// UsageError marks an error as the user's mistake rather than a failure of
// the program: a bad flag, a malformed duration, an unknown command.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

func usagef(format string, args ...any) error {
	return &UsageError{Err: fmt.Errorf(format, args...)}
}

// PreconditionError marks a request that is well-formed but cannot be
// satisfied in the current state.
type PreconditionError struct{ Err error }

func (e *PreconditionError) Error() string { return e.Err.Error() }
func (e *PreconditionError) Unwrap() error { return e.Err }

// ProblemsFoundError reports that a diagnostic command completed successfully
// and found problems. It is not a failure of the command itself.
type ProblemsFoundError struct{ Count int }

func (e *ProblemsFoundError) Error() string {
	return fmt.Sprintf("%d problem(s) found", e.Count)
}

// exitCodeFor maps a domain error onto the public exit-code table.
//
// This is the only place in the program that performs that mapping
// (architecture overview, rule 3). Keeping it in one function is what makes
// the contract testable rather than aspirational.
func exitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}

	var usage *UsageError
	if errors.As(err, &usage) {
		return ExitUsage
	}

	// Domain errors from the core are mapped here and nowhere else, which is
	// what makes the exit-code contract testable rather than aspirational.
	switch {
	case errors.Is(err, app.ErrSessionRunning),
		errors.Is(err, app.ErrNoSession),
		errors.Is(err, app.ErrStopTimeout):
		return ExitPrecondition

	case errors.Is(err, session.ErrInvalidDuration),
		errors.Is(err, session.ErrInvalidMode):
		// A malformed request is the user's mistake, wherever it was caught.
		return ExitUsage
	}

	var precondition *PreconditionError
	if errors.As(err, &precondition) {
		return ExitPrecondition
	}

	var problems *ProblemsFoundError
	if errors.As(err, &problems) {
		return ExitProblemsFound
	}

	return ExitInternal
}
