package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeAtomic writes data to path so that a reader can never observe a partial
// file: it writes a temporary file in the same directory, flushes it to disk,
// and renames it over the target.
//
// There is no partial-write recovery path in Awake because partial writes
// cannot happen. This is the one place worth paying for fsync — state files are
// small and rare, and a torn session record is the hardest thing to reason
// about after a crash. Logs make the opposite trade for the opposite reasons.
//
// The temporary file must share a directory with the target: rename is only
// atomic within a filesystem.
func writeAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".awake-tmp-*")
	if err != nil {
		return fmt.Errorf("creating temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// If anything below fails, leave nothing behind. The named return value is
	// what lets this deferred function see whether the write succeeded.
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", tmpName, err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("flushing %s: %w", tmpName, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}

	// Renaming is durable only once the directory entry itself is flushed.
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("opening %s to flush: %w", dir, err)
	}
	defer d.Close()

	if err := d.Sync(); err != nil {
		return fmt.Errorf("flushing directory %s: %w", dir, err)
	}
	return nil
}
