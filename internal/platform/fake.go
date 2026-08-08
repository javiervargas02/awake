package platform

import (
	"context"
	"fmt"
	"sync"
)

// Fake is a controller for tests.
//
// It lets the core be tested against every platform outcome — unavailable,
// fails to start, dies mid-session, cannot verify — without touching real
// power management, which is otherwise nearly untestable in CI.
type Fake struct {
	mu sync.Mutex

	// Caps is what Describe reports. The zero value is an available,
	// verifiable platform.
	Caps *Capabilities

	// StartErr, if set, is returned by Start.
	StartErr error

	// VerificationResult is reported by handles this controller creates.
	VerificationResult Verification

	// NextPID is the PID given to the next handle.
	NextPID int

	// ReclaimResult and ReclaimErr control what Reclaim reports.
	ReclaimResult bool
	ReclaimErr    error

	starts    int
	handles   []*FakeHandle
	reclaimed []int
}

func NewFake() *Fake {
	return &Fake{VerificationResult: Verified, NextPID: 1000}
}

func (f *Fake) Describe() Capabilities {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Caps != nil {
		return *f.Caps
	}
	return Capabilities{
		Available: true,
		Mechanism: "fake",
		Path:      "/fake/mechanism",
		Supported: []Kind{KindPreventIdleSleep},
		CanVerify: true,
	}
}

func (f *Fake) Start(_ context.Context, req Request) (Handle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	caps := Capabilities{Available: true, Supported: []Kind{KindPreventIdleSleep}}
	if f.Caps != nil {
		caps = *f.Caps
	}
	if !caps.Available {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, caps.Detail)
	}
	if !caps.Supports(req.Kind) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedKind, req.Kind)
	}
	if f.StartErr != nil {
		return nil, f.StartErr
	}

	f.starts++
	f.NextPID++

	h := &FakeHandle{
		pid:          f.NextPID,
		verification: f.VerificationResult,
		died:         make(chan error, 1),
	}
	f.handles = append(f.handles, h)
	return h, nil
}

// Reclaim simulates layer 3 of the lifetime guarantee.
func (f *Fake) Reclaim(_ context.Context, pid int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reclaimed = append(f.reclaimed, pid)

	if f.ReclaimErr != nil {
		return false, f.ReclaimErr
	}
	return f.ReclaimResult, nil
}

// Reclaimed lists the PIDs Reclaim was called with.
func (f *Fake) Reclaimed() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.reclaimed...)
}

// Starts reports how many times Start succeeded.
func (f *Fake) Starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

// Running reports how many handles have not been stopped. A test asserting the
// lifetime guarantee at the core level checks that this reaches zero.
func (f *Fake) Running() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	var running int
	for _, h := range f.handles {
		if !h.Stopped() {
			running++
		}
	}
	return running
}

// LastHandle returns the most recently created handle, for tests that need to
// simulate a mid-session death.
func (f *Fake) LastHandle() *FakeHandle {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.handles) == 0 {
		return nil
	}
	return f.handles[len(f.handles)-1]
}

// FakeHandle is a mechanism that does nothing.
type FakeHandle struct {
	mu           sync.Mutex
	pid          int
	verification Verification
	stopped      bool
	stops        int
	died         chan error
}

func (h *FakeHandle) PID() int { return h.pid }

func (h *FakeHandle) Verification() Verification { return h.verification }

func (h *FakeHandle) Died() <-chan error { return h.died }

func (h *FakeHandle) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.stopped = true
	h.stops++
	return nil
}

func (h *FakeHandle) Stopped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopped
}

// Stops reports how many times Stop was called, so tests can assert
// idempotency rather than assuming it.
func (h *FakeHandle) Stops() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stops
}

// SimulateDeath makes the handle report that the mechanism exited on its own.
func (h *FakeHandle) SimulateDeath(err error) {
	select {
	case h.died <- err:
	default:
	}
}
