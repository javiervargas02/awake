// Package store owns everything Awake keeps on disk.
//
// It implements ADR-0003: every file under the root is expendable, no file
// holds authoritative truth about the program, writes are atomic, and corrupt
// files are set aside rather than destroyed.
//
// One rule shapes the API: reads report, repairs act. A read that finds a
// broken file returns an error describing it and changes nothing. Quarantining,
// regenerating and recreating are separate, explicit operations, because
// silent self-repair is indistinguishable from a bug.
package store

import (
	"errors"
	"os"
	"path/filepath"
)

// Permissions are deliberate. Even with no content logged about the user
// (principle 5), the timing of sessions reveals when someone was away from
// their machine; file modes are the cheapest place to respect that.
const (
	DirPerm  os.FileMode = 0o700
	FilePerm os.FileMode = 0o600
)

var (
	// ErrNotFound means the file is absent. For every file Awake keeps, that
	// is a normal state rather than a failure.
	ErrNotFound = errors.New("not found")

	// ErrCorrupt means the file exists but could not be understood. The
	// caller decides what to do about it; Quarantine is the usual answer.
	ErrCorrupt = errors.New("corrupt")
)

// Store is a rooted view of Awake's state directory.
//
// The root is injected rather than resolved here, so that tests can point at a
// temporary directory and the composition root stays the only place that knows
// about the user's home directory.
type Store struct {
	root string
}

func New(root string) *Store { return &Store{root: root} }

func (s *Store) Root() string          { return s.root }
func (s *Store) ConfigPath() string    { return filepath.Join(s.root, "config.toml") }
func (s *Store) SessionPath() string   { return filepath.Join(s.root, "session.json") }
func (s *Store) UpdatePath() string    { return filepath.Join(s.root, "update.json") }
func (s *Store) LogDir() string        { return filepath.Join(s.root, "logs") }
func (s *Store) SessionLogDir() string { return filepath.Join(s.LogDir(), "sessions") }

// SessionLogPath returns the trace file for one session. The caller passes the
// ID as a string so that this package does not depend on the session package
// merely to build a path.
func (s *Store) SessionLogPath(id string) string {
	return filepath.Join(s.SessionLogDir(), id+".jsonl")
}

// EnsureDirs creates the directory tree with the correct permissions.
//
// Creation is lazy: it happens on the first write that needs it, never on a
// read. Installing Awake and asking it its version leaves no trace on disk.
func (s *Store) EnsureDirs() error {
	for _, dir := range []string{s.root, s.LogDir(), s.SessionLogDir()} {
		if err := os.MkdirAll(dir, DirPerm); err != nil {
			return err
		}
		// MkdirAll respects the process umask, which can produce a directory
		// looser than we asked for. Set the mode explicitly.
		if err := os.Chmod(dir, DirPerm); err != nil {
			return err
		}
	}
	return nil
}

// Exists reports whether a path is present, treating any error other than
// "not exist" as a reason to say yes: a file we cannot stat is not a file we
// should assume is absent.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}
