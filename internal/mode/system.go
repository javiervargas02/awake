package mode

import (
	"context"

	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/session"
)

// System asks the operating system not to sleep while idle.
//
// It is the only mode in v0.1.0 and the only one that involves no synthetic
// input: Awake asks the OS for something and the OS either grants it or does
// not. Nothing is faked, nothing is simulated, and nothing about the user's
// activity is touched.
type System struct {
	controller platform.Controller
}

func NewSystem(controller platform.Controller) *System {
	return &System{controller: controller}
}

func (s *System) Name() session.Mode { return session.ModeSystem }

func (s *System) Mechanism() string { return s.controller.Describe().Mechanism }

func (s *System) Available() (bool, string) {
	caps := s.controller.Describe()
	switch {
	case !caps.Available:
		return false, caps.Detail
	case !caps.Supports(platform.KindPreventIdleSleep):
		return false, "this platform cannot prevent idle sleep"
	default:
		return true, ""
	}
}

func (s *System) Start(ctx context.Context) (platform.Handle, error) {
	return s.controller.Start(ctx, platform.Request{Kind: platform.KindPreventIdleSleep})
}
