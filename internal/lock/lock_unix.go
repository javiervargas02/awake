//go:build unix

package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// New returns a lock at the given path. An empty path means DefaultPath.
func New(path string) Guard {
	if path == "" {
		path = DefaultPath()
	}
	return &fileLock{path: path}
}

type fileLock struct {
	mu   sync.Mutex
	path string
	file *os.File
}

func (l *fileLock) Path() string { return l.path }

func (l *fileLock) TryAcquire() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return true, nil
	}

	if err := os.MkdirAll(filepath.Dir(l.path), dirPerm); err != nil {
		return false, fmt.Errorf("creating lock directory: %w", err)
	}

	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, filePerm)
	if err != nil {
		return false, fmt.Errorf("opening lock file: %w", err)
	}

	// LOCK_NB makes this non-blocking, and the whole operation atomic: the
	// kernel either hands us the lock or tells us someone else has it. There is
	// no moment in between for a second process to occupy.
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, fmt.Errorf("locking %s: %w", l.path, err)
	}

	l.file = file
	return true, nil
}

func (l *fileLock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return nil
	}

	// Closing the descriptor releases the lock on its own; unlocking first is
	// explicit about the intent.
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil

	if unlockErr != nil {
		return fmt.Errorf("unlocking %s: %w", l.path, unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("closing %s: %w", l.path, closeErr)
	}
	return nil
}
