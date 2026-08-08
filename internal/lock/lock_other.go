//go:build !unix

package lock

import (
	"fmt"
	"runtime"
)

// New returns a lock for platforms without advisory file locking.
//
// It always reports that it could not ask, which makes Awake degrade to
// record-based checking and report the weakness through doctor, rather than
// silently pretending exclusivity is enforced.
func New(path string) Guard {
	if path == "" {
		path = DefaultPath()
	}
	return unsupported{path: path}
}

type unsupported struct{ path string }

func (u unsupported) Path() string { return u.path }

func (u unsupported) TryAcquire() (bool, error) {
	return false, fmt.Errorf("advisory locking is not implemented on %s", runtime.GOOS)
}

func (unsupported) Release() error { return nil }
