// Package lock enforces that only one session runs at a time.
//
// It implements ADR-0008. Exclusivity is held as a resource, not derived from
// data: the session takes an OS advisory lock for its whole lifetime.
//
// Two properties are the point. Acquisition is atomic, so there is no
// check-then-act window in which two starts can both conclude the coast is
// clear. And the kernel releases the lock when the holder dies — including
// under SIGKILL, where no cleanup code runs — so a stale lock cannot exist.
//
// The lock deliberately lives outside ~/.awake. That directory is one this
// project actively tells users they may delete; if exclusivity lived there,
// deleting it mid-session would permit a second session.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
)

// Guard is the exclusivity mechanism, as the application sees it.
type Guard interface {
	// TryAcquire takes the lock without blocking.
	//
	// Three outcomes: acquired, not acquired because someone else holds it,
	// or an error meaning we could not even ask. The third degrades rather
	// than refuses — a working tool with a stated weakness beats a tool that
	// will not start because of a temp directory.
	TryAcquire() (bool, error)

	// Release drops the lock. It is idempotent.
	Release() error

	// Path reports where the lock lives, for doctor and for the README.
	Path() string
}

// DefaultPath returns the lock's location: a per-user runtime directory,
// outside ~/.awake and cleared by the operating system on reboot.
//
// On macOS the temporary directory is already per-user and private; the UID in
// the directory name keeps the scoping correct anywhere it is not.
func DefaultPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("awake-%d", os.Getuid()), "session.lock")
}

// dirPerm and filePerm match the rest of Awake's state.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)
