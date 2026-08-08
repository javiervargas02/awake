package mode

import (
	"context"
	"errors"
	"testing"

	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/session"
)

func TestForResolvesSystemMode(t *testing.T) {
	runner, err := For(session.ModeSystem, platform.NewFake())
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	if runner.Name() != session.ModeSystem {
		t.Errorf("name = %q, want %q", runner.Name(), session.ModeSystem)
	}
}

func TestForRejectsUnknownModes(t *testing.T) {
	for _, name := range []session.Mode{"", "mouse", "keyboard", "telepathy"} {
		t.Run(string(name), func(t *testing.T) {
			if _, err := For(name, platform.NewFake()); !errors.Is(err, session.ErrInvalidMode) {
				t.Errorf("For(%q) error = %v, want ErrInvalidMode", name, err)
			}
		})
	}
}

// System mode's whole policy is: ask the platform to prevent idle sleep.
func TestSystemModeRequestsPreventIdleSleep(t *testing.T) {
	fake := platform.NewFake()
	runner := NewSystem(fake)

	handle, err := runner.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer handle.Stop()

	if fake.Starts() != 1 {
		t.Errorf("Starts() = %d, want 1", fake.Starts())
	}
	if fake.Running() != 1 {
		t.Errorf("Running() = %d, want 1", fake.Running())
	}
}

func TestSystemModeReportsMechanism(t *testing.T) {
	runner := NewSystem(platform.NewFake())

	if runner.Mechanism() != "fake" {
		t.Errorf("Mechanism() = %q, want the platform's mechanism name", runner.Mechanism())
	}
}

func TestAvailabilityMirrorsThePlatform(t *testing.T) {
	cases := []struct {
		name      string
		caps      *platform.Capabilities
		available bool
	}{
		{"healthy", nil, true},
		{"unavailable", &platform.Capabilities{Available: false, Detail: "no mechanism here"}, false},
		{"available but cannot prevent idle sleep",
			&platform.Capabilities{Available: true, Supported: []platform.Kind{"something_else"}}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := platform.NewFake()
			fake.Caps = tc.caps

			available, detail := NewSystem(fake).Available()
			if available != tc.available {
				t.Errorf("Available() = %v, want %v", available, tc.available)
			}
			if !available && detail == "" {
				t.Error("an unavailable mode gave no explanation")
			}
		})
	}
}

func TestStartFailureIsPropagated(t *testing.T) {
	fake := platform.NewFake()
	fake.StartErr = errors.New("mechanism refused to start")

	if _, err := NewSystem(fake).Start(context.Background()); err == nil {
		t.Fatal("Start() hid a platform failure")
	}
}

var _ Runner = (*System)(nil)
