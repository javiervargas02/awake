package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/javiervargas02/awake/internal/session"
)

// ReadSession loads the current or most recent session record.
//
// It returns ErrNotFound when no record exists — a normal state, not a
// failure — and ErrCorrupt when one exists but cannot be understood. It never
// repairs, quarantines or rewrites anything.
func (s *Store) ReadSession() (*session.Session, error) {
	data, err := os.ReadFile(s.SessionPath())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("reading session record: %w", err)
	}

	var record session.Session
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("%w: session record is not valid JSON: %v", ErrCorrupt, err)
	}

	// A record can be syntactically valid JSON and still describe something
	// impossible — a running session with an end time, say. The domain decides
	// what is possible, not the store.
	if err := record.Validate(); err != nil {
		return nil, fmt.Errorf("%w: session record is inconsistent: %v", ErrCorrupt, err)
	}

	return &record, nil
}

// WriteSession persists a session record atomically.
//
// It validates first: an invalid record must never reach the disk, because
// the next run would have to treat it as corrupt.
func (s *Store) WriteSession(record *session.Session) error {
	if record == nil {
		return errors.New("cannot write a nil session record")
	}
	if err := record.Validate(); err != nil {
		return fmt.Errorf("refusing to write an invalid session record: %w", err)
	}

	if err := s.EnsureDirs(); err != nil {
		return fmt.Errorf("preparing state directory: %w", err)
	}

	// Indented so that `cat ~/.awake/session.json` is useful to a human
	// (principle 3).
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding session record: %w", err)
	}
	data = append(data, '\n')

	return writeAtomic(s.SessionPath(), data, FilePerm)
}

// ReadJSON decodes any JSON file under the store, applying the same
// not-found/corrupt distinction as ReadSession. Subsystems that own their own
// on-disk types (the update cache, for instance) use this rather than teaching
// the store what their data means.
func (s *Store) ReadJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("reading %s: %w", path, err)
	}

	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("%w: %s is not valid JSON: %v", ErrCorrupt, path, err)
	}
	return nil
}

// WriteJSON encodes a value to a file under the store, atomically.
func (s *Store) WriteJSON(path string, value any) error {
	if err := s.EnsureDirs(); err != nil {
		return fmt.Errorf("preparing state directory: %w", err)
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	data = append(data, '\n')

	return writeAtomic(path, data, FilePerm)
}
