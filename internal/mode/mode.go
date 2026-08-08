// Package mode holds the strategies for keeping a machine awake.
//
// A mode is policy: it decides what approach to take. The platform is
// mechanism: it knows how this operating system does that. Only `system` mode
// exists in v0.1.0, and the abstraction exists anyway — that is the point of
// the milestone, because a mode added later must not change the core.
package mode

import (
	"context"
	"fmt"

	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/session"
)

// Runner starts and describes one way of keeping the machine awake.
type Runner interface {
	// Name is the mode as it appears in state and logs.
	Name() session.Mode

	// Mechanism names the platform facility this mode uses, so a reader can
	// see exactly what Awake asked the OS for rather than inferring it.
	Mechanism() string

	// Available reports whether this mode can run here, and why not if it
	// cannot. doctor uses it before a session is ever attempted.
	Available() (bool, string)

	// Start begins keeping the machine awake.
	Start(ctx context.Context) (platform.Handle, error)
}

// For returns the runner for a mode name.
func For(name session.Mode, controller platform.Controller) (Runner, error) {
	switch name {
	case session.ModeSystem:
		return NewSystem(controller), nil
	default:
		return nil, fmt.Errorf("%w: %q", session.ErrInvalidMode, name)
	}
}
