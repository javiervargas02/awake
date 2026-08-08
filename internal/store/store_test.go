package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javiervargas02/awake/internal/session"
)

var base = time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)

// newStore returns a store rooted in a temporary directory that the testing
// package removes afterwards. Every state test uses a real filesystem: the
// interesting behaviour here is file states and recovery, and mocking the
// filesystem would only test the mock.
func newStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), ".awake"))
}

func runningSession(t *testing.T) *session.Session {
	t.Helper()
	s, err := session.New(base, session.Params{
		Mode:       session.ModeSystem,
		Duration:   30 * time.Minute,
		AppVersion: "test",
		OwnerPID:   os.Getpid(),
	})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}
	return s
}

func TestReadsDoNotCreateAnything(t *testing.T) {
	s := newStore(t)

	if _, err := s.ReadSession(); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReadSession() on empty store = %v, want ErrNotFound", err)
	}
	if Exists(s.Root()) {
		t.Error("a read created the state directory; bootstrap must be lazy")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	s := newStore(t)
	original := runningSession(t)

	if err := s.WriteSession(original); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	loaded, err := s.ReadSession()
	if err != nil {
		t.Fatalf("ReadSession() error = %v", err)
	}
	if loaded.ID != original.ID || loaded.Status != original.Status {
		t.Errorf("round trip changed the record: got %+v", loaded)
	}
	if !loaded.Deadline.Equal(*original.Deadline) {
		t.Errorf("deadline = %v, want %v", loaded.Deadline, original.Deadline)
	}
}

func TestWrittenStateIsReadableByHumans(t *testing.T) {
	s := newStore(t)
	if err := s.WriteSession(runningSession(t)); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	data, err := os.ReadFile(s.SessionPath())
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "\n  \"id\"") {
		t.Error("session record is not indented; `cat` should be useful")
	}
	if !strings.Contains(text, `"requested_duration": "30m0s"`) {
		t.Errorf("duration is not human-readable:\n%s", text)
	}
}

func TestPermissions(t *testing.T) {
	s := newStore(t)
	if err := s.WriteSession(runningSession(t)); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	cases := []struct {
		path string
		want os.FileMode
	}{
		{s.Root(), DirPerm},
		{s.LogDir(), DirPerm},
		{s.SessionLogDir(), DirPerm},
		{s.SessionPath(), FilePerm},
	}

	for _, tc := range cases {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			info, err := os.Stat(tc.path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if got := info.Mode().Perm(); got != tc.want {
				t.Errorf("mode = %04o, want %04o", got, tc.want)
			}
		})
	}
}

func TestReadRejectsCorruptRecords(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"not json", "this is not json at all"},
		{"truncated", `{"id": "20260807t140311z-k3m9x2q7r1", "sta`},
		{"empty", ""},
		{"valid json, impossible record", `{"record_version":1,"id":"20260807t140311z-k3m9x2q7r1",` +
			`"mode":"system","status":"running","started_at":"2026-08-07T14:00:00Z",` +
			`"ended_at":"2026-08-07T14:30:00Z","indefinite":false,"deadline":"2026-08-07T14:30:00Z",` +
			`"requested_duration":"30m0s"}`},
		{"unknown status", `{"record_version":1,"id":"20260807t140311z-k3m9x2q7r1",` +
			`"mode":"system","status":"napping","started_at":"2026-08-07T14:00:00Z",` +
			`"indefinite":false,"deadline":"2026-08-07T14:30:00Z","requested_duration":"30m0s"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			if err := s.EnsureDirs(); err != nil {
				t.Fatalf("EnsureDirs() error = %v", err)
			}
			if err := os.WriteFile(s.SessionPath(), []byte(tc.content), FilePerm); err != nil {
				t.Fatalf("seeding corrupt file: %v", err)
			}

			_, err := s.ReadSession()
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("ReadSession() = %v, want ErrCorrupt", err)
			}

			// Reads report; they never repair. The broken file must still be
			// there, untouched, for the user to inspect.
			after, readErr := os.ReadFile(s.SessionPath())
			if readErr != nil {
				t.Fatalf("a read deleted the corrupt file: %v", readErr)
			}
			if string(after) != tc.content {
				t.Error("a read modified the corrupt file")
			}
		})
	}
}

func TestWriteRejectsInvalidRecords(t *testing.T) {
	s := newStore(t)
	broken := runningSession(t)
	broken.Status = "napping"

	if err := s.WriteSession(broken); err == nil {
		t.Fatal("WriteSession() accepted an invalid record")
	}
	if Exists(s.SessionPath()) {
		t.Error("an invalid record reached the disk")
	}
}

func TestQuarantinePreservesEvidence(t *testing.T) {
	s := newStore(t)
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	const content = "this was not valid json"
	if err := os.WriteFile(s.SessionPath(), []byte(content), FilePerm); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	moved, err := Quarantine(s.SessionPath(), base)
	if err != nil {
		t.Fatalf("Quarantine() error = %v", err)
	}

	if Exists(s.SessionPath()) {
		t.Error("original file still present after quarantine")
	}
	preserved, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("quarantined file unreadable: %v", err)
	}
	if string(preserved) != content {
		t.Error("quarantine altered the file contents")
	}
	if !IsQuarantined(moved) {
		t.Errorf("quarantined path %q is not recognised as quarantined", moved)
	}
}

func TestQuarantineDoesNotCollide(t *testing.T) {
	s := newStore(t)
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	var paths []string
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(s.SessionPath(), []byte("broken"), FilePerm); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		// Same timestamp every time: the collision path is what is under test.
		moved, err := Quarantine(s.SessionPath(), base)
		if err != nil {
			t.Fatalf("Quarantine() error = %v", err)
		}
		paths = append(paths, moved)
	}

	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			t.Fatalf("quarantine reused the path %q and destroyed evidence", p)
		}
		seen[p] = true
	}

	found, err := s.ListQuarantined()
	if err != nil {
		t.Fatalf("ListQuarantined() error = %v", err)
	}
	if len(found) != 3 {
		t.Errorf("ListQuarantined() found %d files, want 3", len(found))
	}
}

func TestAtomicWriteLeavesNoDebris(t *testing.T) {
	s := newStore(t)
	if err := s.WriteSession(runningSession(t)); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	entries, err := os.ReadDir(s.Root())
	if err != nil {
		t.Fatalf("reading root: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".awake-tmp-") {
			t.Errorf("temporary file %q survived the write", e.Name())
		}
	}
}

func TestOverwriteIsAtomicallyReplaced(t *testing.T) {
	s := newStore(t)

	first := runningSession(t)
	if err := s.WriteSession(first); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	second := runningSession(t)
	if err := second.Stop(base.Add(time.Minute), session.ReasonUserStopped); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := s.WriteSession(second); err != nil {
		t.Fatalf("second WriteSession() error = %v", err)
	}

	loaded, err := s.ReadSession()
	if err != nil {
		t.Fatalf("ReadSession() error = %v", err)
	}
	if loaded.ID != second.ID {
		t.Error("the second write did not replace the first")
	}
	if loaded.Status != session.StatusStopped {
		t.Errorf("status = %q, want %q", loaded.Status, session.StatusStopped)
	}
}

// Deleting ~/.awake between commands must never break the next one.
func TestRecoversFromDeletedRoot(t *testing.T) {
	s := newStore(t)

	if err := s.WriteSession(runningSession(t)); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}
	if err := os.RemoveAll(s.Root()); err != nil {
		t.Fatalf("removing root: %v", err)
	}

	if _, err := s.ReadSession(); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReadSession() after deletion = %v, want ErrNotFound", err)
	}
	if err := s.WriteSession(runningSession(t)); err != nil {
		t.Fatalf("WriteSession() after deletion = %v", err)
	}
	if _, err := s.ReadSession(); err != nil {
		t.Errorf("ReadSession() after recreation = %v", err)
	}
}

func TestGenericJSONHelpers(t *testing.T) {
	s := newStore(t)
	type cache struct {
		Version int    `json:"version"`
		Result  string `json:"result"`
	}

	if err := s.ReadJSON(s.UpdatePath(), &cache{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReadJSON() on missing file = %v, want ErrNotFound", err)
	}

	if err := s.WriteJSON(s.UpdatePath(), cache{Version: 1, Result: "up_to_date"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}

	var loaded cache
	if err := s.ReadJSON(s.UpdatePath(), &loaded); err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	if loaded.Result != "up_to_date" {
		t.Errorf("result = %q, want %q", loaded.Result, "up_to_date")
	}

	if err := os.WriteFile(s.UpdatePath(), []byte("{nope"), FilePerm); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if err := s.ReadJSON(s.UpdatePath(), &loaded); !errors.Is(err, ErrCorrupt) {
		t.Errorf("ReadJSON() on corrupt file = %v, want ErrCorrupt", err)
	}
}

func TestEnsureDirsIsIdempotent(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 3; i++ {
		if err := s.EnsureDirs(); err != nil {
			t.Fatalf("EnsureDirs() call %d error = %v", i+1, err)
		}
	}
}

func TestEnsureDirsTightensLoosePermissions(t *testing.T) {
	s := newStore(t)
	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := os.Chmod(s.Root(), 0o755); err != nil {
		t.Fatalf("loosening permissions: %v", err)
	}

	if err := s.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	info, err := os.Stat(s.Root())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != DirPerm {
		t.Errorf("mode = %04o, want %04o", got, DirPerm)
	}
}
