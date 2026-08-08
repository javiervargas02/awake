// Package platform is the boundary between Awake and the operating system.
//
// It implements ADR-0006. The interface is deliberately narrow — describe,
// start, stop — and the layer knows nothing about sessions: no deadlines, no
// end reasons, no config, no logging. It starts something, stops something,
// and reports what it can do.
//
// The hard requirement this package exists to uphold: no process Awake starts
// may outlive Awake. An orphaned assertion means a machine that will not sleep,
// with no visible cause and no obvious way to stop it.
package platform

import (
	"context"
	"errors"
)

// Kind is a variety of keep-awake. Only one exists in v0.1.0.
type Kind string

const (
	// KindPreventIdleSleep stops the system sleeping when idle. It does not
	// keep the display awake: that has real costs — power, and a screen left
	// visible in a room the user has walked away from — and is a different
	// user intent, deferred to v0.2.
	KindPreventIdleSleep Kind = "prevent_idle_sleep"
)

var (
	// ErrUnavailable means this platform cannot keep the machine awake.
	ErrUnavailable = errors.New("keep-awake mechanism unavailable")

	// ErrAssertionAbsent means the mechanism started but the operating system
	// reports no assertion for it. A live child process is evidence, not proof;
	// this is the case where the proof came back negative.
	ErrAssertionAbsent = errors.New("mechanism started but no assertion was registered")

	// ErrUnsupportedKind means this platform cannot provide the requested kind.
	ErrUnsupportedKind = errors.New("unsupported keep-awake kind")
)

// Capabilities describes what this platform can do, so that doctor can report
// an unavailable mechanism before a session is attempted rather than during
// one.
type Capabilities struct {
	Available bool
	Mechanism string
	Path      string
	Supported []Kind

	// CanVerify reports whether the assertion can be confirmed with the OS
	// after starting. A machine that cannot verify says so up front.
	CanVerify bool

	// Detail explains an unavailable or degraded platform in plain language.
	Detail string
}

func (c Capabilities) Supports(kind Kind) bool {
	for _, supported := range c.Supported {
		if supported == kind {
			return true
		}
	}
	return false
}

// Verification records whether the assertion was confirmed.
//
// There are three possible outcomes and only two appear here: a query that
// works and finds nothing is a failure, reported as ErrAssertionAbsent rather
// than as a handle. The distinction that survives is between "confirmed" and
// "could not ask" — because if Awake cannot ask the question, it must not
// answer it.
type Verification string

const (
	// Verified: the OS confirms an assertion attributable to this process.
	Verified Verification = "verified"
	// Unverifiable: the query itself failed. The session proceeds, logged at
	// warn. Failing a session that is probably working, because a diagnostic
	// query broke, would be its own trust failure.
	Unverifiable Verification = "unverifiable"
)

// Request describes what the caller wants started.
type Request struct {
	Kind Kind

	// WatchPID is the process the mechanism should outlive nothing beyond.
	// Zero means the current process, which is what production always uses;
	// tests set it so the lifetime tie can be exercised against a process they
	// are willing to kill.
	WatchPID int
}

// Handle represents a running mechanism.
type Handle interface {
	// PID identifies the mechanism process, so a crashed session's leftovers
	// can be found and reclaimed on the next run.
	PID() int

	// Verification reports whether the assertion was confirmed at start.
	Verification() Verification

	// Died delivers one value if the mechanism exits unexpectedly. It never
	// fires as a result of Stop.
	Died() <-chan error

	// Stop releases the mechanism. It is idempotent: shutdown paths overlap,
	// and stopping an already-stopped handle is not an error.
	Stop() error
}

// Controller is the platform's whole surface.
type Controller interface {
	Describe() Capabilities
	Start(ctx context.Context, req Request) (Handle, error)

	// Reclaim terminates a mechanism left behind by a previous run — layer 3
	// of the lifetime guarantee. It reports whether it actually stopped
	// something.
	//
	// It belongs here because identifying a process is platform-specific, and
	// because of the rule it must enforce: never terminate a PID that cannot
	// be verified as the expected executable. PIDs are reused, and killing an
	// innocent process to protect a guarantee would be a far worse bug than
	// the one being prevented.
	Reclaim(ctx context.Context, pid int) (bool, error)
}
