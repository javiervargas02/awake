package platform

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeStartAndStop(t *testing.T) {
	f := NewFake()

	handle, err := f.Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if handle.PID() == 0 {
		t.Error("handle has no PID; startup reclamation would have nothing to look for")
	}
	if handle.Verification() != Verified {
		t.Errorf("verification = %q, want %q", handle.Verification(), Verified)
	}
	if f.Running() != 1 {
		t.Errorf("Running() = %d, want 1", f.Running())
	}

	if err := handle.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if f.Running() != 0 {
		t.Error("mechanism still running after Stop()")
	}
}

// Shutdown paths overlap, so stopping twice must be safe.
func TestStopIsIdempotent(t *testing.T) {
	f := NewFake()

	handle, err := f.Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := handle.Stop(); err != nil {
			t.Fatalf("Stop() call %d error = %v", i+1, err)
		}
	}
	if f.Running() != 0 {
		t.Error("mechanism still running")
	}
}

func TestUnavailablePlatformFailsClearly(t *testing.T) {
	f := NewFake()
	f.Caps = &Capabilities{Available: false, Detail: "no mechanism on this machine"}

	if _, err := f.Start(context.Background(), Request{Kind: KindPreventIdleSleep}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Start() error = %v, want ErrUnavailable", err)
	}
}

func TestUnsupportedKindIsRejected(t *testing.T) {
	f := NewFake()

	_, err := f.Start(context.Background(), Request{Kind: "keep_display_awake"})
	if !errors.Is(err, ErrUnsupportedKind) {
		t.Fatalf("Start() error = %v, want ErrUnsupportedKind", err)
	}
}

func TestUnverifiablePlatformStillStarts(t *testing.T) {
	f := NewFake()
	f.VerificationResult = Unverifiable

	handle, err := f.Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		t.Fatalf("Start() error = %v; an unverifiable platform must still run", err)
	}
	if handle.Verification() != Unverifiable {
		t.Errorf("verification = %q, want %q", handle.Verification(), Unverifiable)
	}
}

func TestDeathIsReported(t *testing.T) {
	f := NewFake()

	handle, err := f.Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	handle.(*FakeHandle).SimulateDeath(errors.New("mechanism exited"))

	select {
	case err := <-handle.Died():
		if err == nil {
			t.Error("Died() delivered a nil error")
		}
	case <-time.After(time.Second):
		t.Fatal("Died() never fired")
	}
}

func TestCapabilitiesSupports(t *testing.T) {
	caps := Capabilities{Supported: []Kind{KindPreventIdleSleep}}

	if !caps.Supports(KindPreventIdleSleep) {
		t.Error("Supports() said no to a supported kind")
	}
	if caps.Supports("keep_display_awake") {
		t.Error("Supports() said yes to an unsupported kind")
	}
}

// Both real controllers must satisfy the interface. This fails at compile time
// if either drifts.
var (
	_ Controller = (*Fake)(nil)
	_ Handle     = (*FakeHandle)(nil)
)
