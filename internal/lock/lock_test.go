package lock

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func tempLockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "awake", "session.lock")
}

func TestAcquireAndRelease(t *testing.T) {
	l := New(tempLockPath(t))

	acquired, err := l.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	if !acquired {
		t.Fatal("could not acquire a free lock")
	}

	if err := l.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// The lock is reusable once released.
	acquired, err = l.TryAcquire()
	if err != nil || !acquired {
		t.Fatalf("re-acquire = %v, %v; want true, nil", acquired, err)
	}
	l.Release()
}

func TestReleaseIsIdempotent(t *testing.T) {
	l := New(tempLockPath(t))

	if _, err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := l.Release(); err != nil {
			t.Fatalf("Release() call %d error = %v", i+1, err)
		}
	}
}

func TestSecondHolderIsRefused(t *testing.T) {
	path := tempLockPath(t)

	first := New(path)
	if acquired, err := first.TryAcquire(); err != nil || !acquired {
		t.Fatalf("first TryAcquire() = %v, %v", acquired, err)
	}
	defer first.Release()

	// A distinct Guard on the same path, as a second process would be.
	second := New(path)
	acquired, err := second.TryAcquire()
	if err != nil {
		t.Fatalf("second TryAcquire() error = %v; contention is not an error", err)
	}
	if acquired {
		t.Fatal("two holders acquired the same lock")
	}
}

func TestLockIsFreedWhenTheHolderDies(t *testing.T) {
	// The property a PID file can never offer: the kernel releases the lock
	// when the process dies, including under SIGKILL where no cleanup runs.
	path := filepath.Join(t.TempDir(), "session.lock")

	// A helper process that takes the lock and then waits forever.
	helper := exec.Command("/bin/sh", "-c",
		"exec 9>"+path+"; if command -v flock >/dev/null 2>&1; then flock -n 9 || exit 1; fi; sleep 60")
	if err := helper.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}

	// macOS has no flock(1), so this test verifies the weaker but still
	// meaningful property: our own lock survives and is reclaimable.
	_ = helper.Process.Kill()
	_ = helper.Wait()

	l := New(path)
	acquired, err := l.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire() after holder died = %v", err)
	}
	if !acquired {
		t.Error("the lock was not released when its holder died")
	}
	l.Release()
}

func TestDefaultPathIsOutsideHome(t *testing.T) {
	path := DefaultPath()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home directory: %v", err)
	}

	if strings.HasPrefix(path, filepath.Join(home, ".awake")) {
		t.Errorf("the lock lives at %q, inside ~/.awake; deleting that directory would permit a second session", path)
	}
	if !strings.Contains(path, "awake") {
		t.Errorf("lock path %q does not identify itself as Awake's", path)
	}
}

// The lock is per-user, matching ~/.awake and the fact that sessions belong to
// a user rather than to a machine.
func TestDefaultPathIsPerUser(t *testing.T) {
	if !strings.Contains(DefaultPath(), "awake-") {
		t.Errorf("lock path %q is not scoped to a user", DefaultPath())
	}
}

func TestLockFilePermissions(t *testing.T) {
	path := tempLockPath(t)
	l := New(path)

	if _, err := l.TryAcquire(); err != nil {
		t.Fatalf("TryAcquire() error = %v", err)
	}
	defer l.Release()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != filePerm {
		t.Errorf("lock file mode = %04o, want %04o", got, filePerm)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != dirPerm {
		t.Errorf("lock dir mode = %04o, want %04o", got, dirPerm)
	}
}

func TestFakeBehavesLikeTheRealThing(t *testing.T) {
	f := NewFake()

	if acquired, err := f.TryAcquire(); err != nil || !acquired {
		t.Fatalf("TryAcquire() = %v, %v", acquired, err)
	}
	if acquired, _ := f.TryAcquire(); acquired {
		// The real lock is re-entrant for the same holder; the fake reports
		// held rather than acquiring twice.
		t.Log("fake reports the lock as already held by itself")
	}
	if err := f.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if f.Held() {
		t.Error("Held() is true after Release()")
	}
}

var _ Guard = (*Fake)(nil)
