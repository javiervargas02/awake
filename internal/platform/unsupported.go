//go:build !darwin

package platform

import (
	"context"
	"fmt"
	"runtime"
)

// New returns a controller for platforms Awake does not support yet.
//
// The MVP is macOS-only. This exists so the project still builds and its tests
// still run elsewhere — a contributor on Linux can work on everything above
// this layer — and so that an unsupported platform is a clear diagnosis rather
// than a confusing runtime failure.
func New() Controller { return &unsupported{} }

type unsupported struct{}

func (unsupported) Describe() Capabilities {
	return Capabilities{
		Available: false,
		Mechanism: "none",
		Detail: fmt.Sprintf(
			"Awake does not support %s yet; the MVP targets macOS only", runtime.GOOS),
	}
}

func (u unsupported) Start(context.Context, Request) (Handle, error) {
	return nil, fmt.Errorf("%w: %s", ErrUnavailable, u.Describe().Detail)
}

func (unsupported) Reclaim(context.Context, int) (bool, error) {
	// Nothing was ever started here, so there is nothing to reclaim.
	return false, nil
}
