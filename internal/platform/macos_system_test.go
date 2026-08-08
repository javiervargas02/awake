//go:build darwin && system

// These tests drive real macOS power assertions. They are behind the `system`
// build tag so they never run by accident, and an explicit CI job runs them —
// "forgotten" should show up as a missing job rather than as silence.
//
//	make test-system

package platform

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// assertionsFor asks the live system whether a process holds our assertion.
func assertionsFor(t *testing.T, pid int) bool {
	t.Helper()

	output, err := exec.Command(pmsetPath, "-g", "assertions").Output()
	if err != nil {
		t.Fatalf("querying assertions: %v", err)
	}
	return assertionHeldBy(string(output), pid)
}

func processAlive(pid int) bool {
	// Signal 0 performs error checking without sending anything.
	return syscall.Kill(pid, 0) == nil
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return condition()
}

func TestRealAssertionIsHeldAndReleased(t *testing.T) {
	controller := New()

	handle, err := controller.Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if handle.Verification() != Verified {
		t.Errorf("verification = %q, want %q on a healthy machine", handle.Verification(), Verified)
	}
	if !assertionsFor(t, handle.PID()) {
		t.Error("the system reports no assertion for the mechanism we started")
	}

	if err := handle.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if !waitUntil(t, 3*time.Second, func() bool { return !processAlive(handle.PID()) }) {
		t.Error("mechanism process survived Stop()")
	}
	if assertionsFor(t, handle.PID()) {
		t.Error("the assertion outlived the session")
	}
}

// The orphan guarantee, layer 1.
//
// This is the test that cannot be faked and the one ADR-0006 makes mandatory.
// The mechanism is tied to a process we then SIGKILL, so none of Awake's own
// cleanup code runs — exactly the case where an orphaned assertion would leave
// a machine that will not sleep, with no visible cause.
func TestMechanismDiesWithTheProcessItWatches(t *testing.T) {
	// A stand-in for an Awake process, which we are willing to kill outright.
	victim := exec.Command("/bin/sleep", "600")
	if err := victim.Start(); err != nil {
		t.Fatalf("starting victim process: %v", err)
	}
	victimPID := victim.Process.Pid
	defer func() {
		_ = victim.Process.Kill()
		_ = victim.Wait()
	}()

	handle, err := New().Start(context.Background(), Request{
		Kind:     KindPreventIdleSleep,
		WatchPID: victimPID,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !assertionsFor(t, handle.PID()) {
		t.Fatal("no assertion registered before the kill")
	}

	// No cleanup code of ours runs after this line.
	if err := victim.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("killing victim: %v", err)
	}
	_ = victim.Wait()

	if !waitUntil(t, 5*time.Second, func() bool { return !processAlive(handle.PID()) }) {
		// Leave nothing behind even when the guarantee fails.
		_ = handle.Stop()
		t.Fatal("ORPHAN: the mechanism outlived the process it was watching")
	}
	if assertionsFor(t, handle.PID()) {
		t.Error("ORPHAN: the assertion survived the process it was tied to")
	}
}

// An unexpected exit must be surfaced, not absorbed. Awake never reports a
// session as running while failing to keep the machine awake.
func TestUnexpectedDeathIsReported(t *testing.T) {
	handle, err := New().Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer handle.Stop()

	// Kill the mechanism behind Awake's back.
	if err := syscall.Kill(handle.PID(), syscall.SIGKILL); err != nil {
		t.Fatalf("killing mechanism: %v", err)
	}

	select {
	case err := <-handle.Died():
		if err == nil {
			t.Error("Died() reported a nil error for an unexpected exit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the mechanism died and nothing was reported")
	}
}

// Stopping must not be reported as an unexpected death.
func TestStopIsNotReportedAsDeath(t *testing.T) {
	handle, err := New().Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := handle.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	select {
	case err := <-handle.Died():
		t.Errorf("Stop() was reported as an unexpected death: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestConcurrentMechanismsAreIndependent(t *testing.T) {
	controller := New()

	first, err := controller.Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	second, err := controller.Start(context.Background(), Request{Kind: KindPreventIdleSleep})
	if err != nil {
		first.Stop()
		t.Fatalf("second Start() error = %v", err)
	}

	if first.PID() == second.PID() {
		t.Fatal("two mechanisms share a PID")
	}

	if err := first.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !assertionsFor(t, second.PID()) {
		t.Error("stopping one mechanism released another's assertion")
	}

	if err := second.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if !waitUntil(t, 3*time.Second, func() bool { return !processAlive(second.PID()) }) {
		t.Error("second mechanism survived Stop()")
	}
}

func TestVerificationRejectsAWrongPID(t *testing.T) {
	// A PID that holds nothing must not verify, even on a machine where some
	// other process is legitimately preventing sleep.
	unused := 999999
	if _, err := strconv.Atoi("999999"); err != nil {
		t.Fatal(err)
	}
	if assertionsFor(t, unused) {
		t.Errorf("pid %d appears to hold an assertion", unused)
	}
}
