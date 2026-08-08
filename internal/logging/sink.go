package logging

import (
	"fmt"
	"os"
	"path/filepath"
)

// File permissions match the rest of Awake's state. Even with no content
// logged about the user, the timing of sessions reveals when someone was away
// from their machine.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// OpenFile opens a log file for appending, creating its directory if needed.
//
// Append mode is what makes concurrent writers safe: two processes appending
// whole lines cannot interleave into a corrupt line. There is no buffering
// wrapper, deliberately — one event is one write syscall, so everything logged
// before a SIGKILL is on disk. A buffered writer would lose exactly the events
// preceding a crash, which are the ones that explain it.
func OpenFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return nil, fmt.Errorf("opening log file %s: %w", path, err)
	}
	return file, nil
}
