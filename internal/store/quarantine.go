package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// quarantineSuffix is the marker that identifies a set-aside file. It is also
// what `repair --clean-quarantine` matches on, so it must be narrow enough
// that the deletion it authorises can be described in one sentence.
const quarantineSuffix = ".corrupt-"

const quarantineStamp = "20060102t150405z"

// Quarantine renames a file that could not be understood, so that recovery
// never destroys evidence (ADR-0003). It returns the new path.
//
// This is a repair action, never something a read performs on its own.
func Quarantine(path string, now time.Time) (string, error) {
	base := path + quarantineSuffix + now.UTC().Format(quarantineStamp)

	target := base
	for attempt := 1; Exists(target); attempt++ {
		if attempt > 100 {
			return "", fmt.Errorf("quarantining %s: too many existing files named %s", path, base)
		}
		target = fmt.Sprintf("%s-%d", base, attempt)
	}

	if err := os.Rename(path, target); err != nil {
		return "", fmt.Errorf("quarantining %s: %w", path, err)
	}
	return target, nil
}

// IsQuarantined reports whether a path is a file Awake previously set aside.
func IsQuarantined(path string) bool {
	return strings.Contains(filepath.Base(path), quarantineSuffix)
}

// ListQuarantined returns every set-aside file under the root.
//
// Quarantined files are never removed automatically: `doctor` reports how many
// exist, and only `repair --clean-quarantine` deletes them. Deleting a user's
// file is their decision.
func (s *Store) ListQuarantined() ([]string, error) {
	var found []string

	err := filepath.WalkDir(s.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// A directory we cannot read is a problem for doctor to report,
			// not a reason to abandon the whole walk.
			return nil
		}
		if !entry.IsDir() && IsQuarantined(path) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning for quarantined files: %w", err)
	}
	return found, nil
}
