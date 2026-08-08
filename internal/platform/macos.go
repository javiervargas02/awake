//go:build darwin

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Both tools are addressed by absolute path, never through a PATH lookup. A
// tool that spawns a system utility while the user is away must not be
// redirectable by an environment variable.
const (
	caffeinatePath = "/usr/bin/caffeinate"
	pmsetPath      = "/usr/bin/pmset"

	// assertionName is what macOS calls the assertion we ask for.
	assertionName = "PreventUserIdleSystemSleep"
)

// Verification is retried briefly: assertion registration is not instantaneous.
const (
	verifyAttempts = 10
	verifyInterval = 50 * time.Millisecond
	verifyTimeout  = 2 * time.Second
)

// stopGrace is how long a mechanism gets to exit politely before it is killed.
const stopGrace = 2 * time.Second

// New returns the controller for this platform.
func New() Controller { return &macOS{} }

type macOS struct{}

func (m *macOS) Describe() Capabilities {
	caps := Capabilities{
		Mechanism: "caffeinate",
		Path:      caffeinatePath,
		Supported: []Kind{KindPreventIdleSleep},
	}

	if err := executable(caffeinatePath); err != nil {
		caps.Detail = fmt.Sprintf("%s is not usable: %v", caffeinatePath, err)
		return caps
	}
	caps.Available = true

	if err := executable(pmsetPath); err != nil {
		caps.Detail = fmt.Sprintf("assertions cannot be verified: %s is not usable: %v", pmsetPath, err)
		return caps
	}
	caps.CanVerify = true

	return caps
}

func executable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("not an executable file")
	}
	return nil
}

// Start asks macOS to stop sleeping when idle, and confirms that it did.
func (m *macOS) Start(ctx context.Context, req Request) (Handle, error) {
	caps := m.Describe()
	if !caps.Available {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, caps.Detail)
	}
	if !caps.Supports(req.Kind) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedKind, req.Kind)
	}

	watch := req.WatchPID
	if watch == 0 {
		watch = os.Getpid()
	}

	// Layer 1 of the lifetime guarantee: -w ties the mechanism's life to a
	// process. caffeinate holds its assertion only while that process lives and
	// exits when it does — including when the process is SIGKILLed and no
	// cleanup code of ours runs at all. This is the layer that matters, because
	// it does not depend on Awake getting a chance to do anything.
	cmd := exec.Command(caffeinatePath, "-i", "-w", fmt.Sprint(watch))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", caffeinatePath, err)
	}

	h := &macHandle{
		cmd:  cmd,
		pid:  cmd.Process.Pid,
		done: make(chan struct{}),
		died: make(chan error, 1),
	}
	go h.reap()

	verification, err := verifyAssertion(ctx, h.pid, caps.CanVerify)
	if err != nil {
		// Started but demonstrably doing nothing: leave nothing behind.
		_ = h.Stop()
		return nil, err
	}
	h.verification = verification

	return h, nil
}

// verifyAssertion confirms the OS registered an assertion for the mechanism.
//
// The three outcomes are kept distinct on purpose. Verified proceeds. Verified
// absent fails the session. Unverifiable proceeds with a warning, because
// saying "running, but I could not confirm it" is honest and saying "failed"
// would not be.
func verifyAssertion(ctx context.Context, mechanismPID int, canVerify bool) (Verification, error) {
	if !canVerify {
		return Unverifiable, nil
	}

	deadline, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt < verifyAttempts; attempt++ {
		output, err := exec.CommandContext(deadline, pmsetPath, "-g", "assertions").Output()
		if err != nil {
			lastErr = err
		} else if assertionHeldBy(string(output), mechanismPID) {
			return Verified, nil
		} else {
			lastErr = nil
		}

		select {
		case <-deadline.Done():
			// Out of time. If the query never worked, we could not ask; if it
			// worked and found nothing, we asked and the answer was no.
			if lastErr != nil {
				return Unverifiable, nil
			}
			return "", fmt.Errorf("%w: no %s assertion for pid %d", ErrAssertionAbsent, assertionName, mechanismPID)
		case <-time.After(verifyInterval):
		}
	}

	if lastErr != nil {
		return Unverifiable, nil
	}
	return "", fmt.Errorf("%w: no %s assertion for pid %d", ErrAssertionAbsent, assertionName, mechanismPID)
}

// assertionHeldBy reports whether pmset's output shows our assertion owned by
// the given process.
//
// Attributing the assertion to our own process matters: a machine may be held
// awake by something else entirely, and "somebody is preventing sleep" is not
// the same claim as "Awake is preventing sleep".
func assertionHeldBy(output string, pid int) bool {
	needle := fmt.Sprintf("pid %d(", pid)

	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, needle) && strings.Contains(line, assertionName) {
			return true
		}
	}
	return false
}

// Reclaim stops a mechanism left behind by a previous run.
//
// The identity check is the important part. A recorded PID may since have been
// reused by something entirely unrelated, so the process is confirmed to be
// caffeinate before anything is signalled. If it cannot be confirmed, Reclaim
// does nothing and says so — doctor reports the anomaly rather than Awake
// killing a stranger's process.
func (m *macOS) Reclaim(ctx context.Context, pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}

	// Signal 0 checks for existence without sending anything.
	if err := syscall.Kill(pid, 0); err != nil {
		return false, nil // already gone
	}

	output, err := exec.CommandContext(ctx, "/bin/ps", "-p", fmt.Sprint(pid), "-o", "comm=").Output()
	if err != nil {
		return false, fmt.Errorf("identifying process %d: %w", pid, err)
	}

	name := strings.TrimSpace(string(output))
	if name != caffeinatePath && !strings.HasSuffix(name, "/caffeinate") && name != "caffeinate" {
		return false, fmt.Errorf("process %d is %q, not the keep-awake mechanism; refusing to terminate it", pid, name)
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return false, fmt.Errorf("terminating orphaned mechanism %d: %w", pid, err)
	}

	for waited := time.Duration(0); waited < stopGrace; waited += 50 * time.Millisecond {
		if syscall.Kill(pid, 0) != nil {
			return true, nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The guarantee is not best-effort.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return true, nil
}

type macHandle struct {
	cmd *exec.Cmd
	pid int

	verification Verification

	stopping atomic.Bool
	done     chan struct{}
	died     chan error
	stopOnce sync.Once
}

func (h *macHandle) PID() int                   { return h.pid }
func (h *macHandle) Verification() Verification { return h.verification }
func (h *macHandle) Died() <-chan error         { return h.died }

// reap waits for the mechanism and reports an exit nobody asked for.
//
// Awake does not restart a mechanism that died: something that failed once will
// likely fail again, and a session flapping between working and not is
// unexplainable from its logs. Ending honestly beats continuing dishonestly.
func (h *macHandle) reap() {
	err := h.cmd.Wait()
	close(h.done)

	if h.stopping.Load() {
		return
	}

	if err == nil {
		err = errors.New("keep-awake mechanism exited unexpectedly")
	} else {
		err = fmt.Errorf("keep-awake mechanism exited unexpectedly: %w", err)
	}

	select {
	case h.died <- err:
	default:
	}
}

// Stop releases the mechanism and waits for it to be gone. It is layer 2 of the
// lifetime guarantee: the fast, clean path that runs on every ordinary exit.
func (h *macHandle) Stop() error {
	h.stopOnce.Do(func() {
		h.stopping.Store(true)

		if h.cmd.Process == nil {
			return
		}
		_ = h.cmd.Process.Signal(syscall.SIGTERM)

		select {
		case <-h.done:
		case <-time.After(stopGrace):
			// The guarantee is not best-effort. If it will not leave politely,
			// it leaves anyway.
			_ = h.cmd.Process.Kill()
			<-h.done
		}
	})
	return nil
}
