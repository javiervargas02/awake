package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javiervargas02/awake/internal/clock"
)

var base = time.Date(2026, 8, 7, 14, 3, 11, 482913000, time.UTC)

func newTestLogger(t *testing.T) (*Logger, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var global, stderr bytes.Buffer
	return New(Options{
		Clock:  clock.NewFake(base),
		Global: &global,
		Stderr: &stderr,
	}), &global, &stderr
}

// decodeLines parses JSON Lines output, failing the test on the first line that
// is not a complete JSON object.
func decodeLines(t *testing.T, raw string) []map[string]any {
	t.Helper()

	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line is not valid JSON: %v\n%s", err, line)
		}
		events = append(events, event)
	}
	return events
}

func TestEnvelope(t *testing.T) {
	logger, global, _ := newTestLogger(t)

	logger.Info(EventAppStarted, Fields{"app_version": "0.1.0", "command": "start"})

	events := decodeLines(t, global.String())
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	event := events[0]

	if got := event["ts"]; got != "2026-08-07T14:03:11.482913Z" {
		t.Errorf("ts = %v, want microsecond precision in UTC", got)
	}
	if got := event["schema_version"]; got != float64(SchemaVersion) {
		t.Errorf("schema_version = %v, want %d", got, SchemaVersion)
	}
	if got := event["level"]; got != string(LevelInfo) {
		t.Errorf("level = %v, want %q", got, LevelInfo)
	}
	if got := event["event"]; got != EventAppStarted {
		t.Errorf("event = %v, want %q", got, EventAppStarted)
	}
	if _, present := event["session_id"]; present {
		t.Error("a non-session event carries session_id")
	}

	data, ok := event["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %v, want an object", event["data"])
	}
	if data["command"] != "start" {
		t.Errorf("data.command = %v, want %q", data["command"], "start")
	}
}

// There is deliberately no free-text message field: prose is a rendering
// concern, and a message would be a second copy of facts already in the data.
func TestNoMessageField(t *testing.T) {
	logger, global, _ := newTestLogger(t)
	logger.Info(EventSessionStarted, nil)

	for _, event := range decodeLines(t, global.String()) {
		for _, forbidden := range []string{"message", "msg", "text"} {
			if _, present := event[forbidden]; present {
				t.Errorf("envelope carries a %q field", forbidden)
			}
		}
	}
}

// Session-scoped events go to both destinations: the global log stays a
// complete timeline, the trace stays independently readable.
func TestSessionEventsGoToBothSinks(t *testing.T) {
	logger, global, _ := newTestLogger(t)
	var trace bytes.Buffer

	sessionLogger := logger.WithSession("20260807t140311z-k3m9x2q7r1", &trace)
	sessionLogger.Info(EventSessionStarted, nil)

	globalEvents := decodeLines(t, global.String())
	traceEvents := decodeLines(t, trace.String())

	if len(globalEvents) != 1 || len(traceEvents) != 1 {
		t.Fatalf("global has %d events, trace has %d; want 1 each",
			len(globalEvents), len(traceEvents))
	}
	if globalEvents[0]["session_id"] != "20260807t140311z-k3m9x2q7r1" {
		t.Errorf("session_id = %v", globalEvents[0]["session_id"])
	}
	if traceEvents[0]["event"] != globalEvents[0]["event"] {
		t.Error("the two sinks disagree about what happened")
	}
}

func TestNonSessionEventsSkipTheTrace(t *testing.T) {
	logger, global, _ := newTestLogger(t)
	var trace bytes.Buffer

	sessionLogger := logger.WithSession("session-id", &trace)
	sessionLogger.Info(EventSessionStarted, nil)

	// The parent logger is not session-bound; its events must not reach the
	// session's trace.
	logger.Info(EventUpdateCheckStarted, Fields{"channel": "stable"})

	if len(decodeLines(t, trace.String())) != 1 {
		t.Errorf("trace received a non-session event:\n%s", trace.String())
	}
	if len(decodeLines(t, global.String())) != 2 {
		t.Errorf("global log is missing events:\n%s", global.String())
	}
}

// failingWriter fails every write, standing in for a full disk or a revoked
// permission.
type failingWriter struct{ writes int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.writes++
	return 0, errors.New("no space left on device")
}

// Logging must never be the reason a session ends.
func TestTraceFailureIsRecordedAndSurvivable(t *testing.T) {
	logger, global, stderr := newTestLogger(t)
	trace := &failingWriter{}

	sessionLogger := logger.WithSession("session-id", trace)
	sessionLogger.Info(EventSessionStarted, nil)
	sessionLogger.Info(EventSessionStopped, Fields{"end_reason": "user_stopped", "elapsed": "1m0s"})

	events := decodeLines(t, global.String())

	var sawSinkFailure bool
	for _, event := range events {
		if event["event"] == EventLogSinkFailed {
			sawSinkFailure = true
			data := event["data"].(map[string]any)
			if data["sink"] != "session_trace" {
				t.Errorf("sink = %v, want session_trace", data["sink"])
			}
		}
	}
	if !sawSinkFailure {
		t.Error("a failing trace was not reported in the global log")
	}

	// One complaint, not one per event.
	if trace.writes != 1 {
		t.Errorf("trace was written %d times after failing; want 1", trace.writes)
	}
	if stderr.Len() != 0 {
		t.Errorf("a failing trace disturbed stderr: %s", stderr.String())
	}

	// Both events still reached the global log.
	var stopped bool
	for _, event := range events {
		if event["event"] == EventSessionStopped {
			stopped = true
		}
	}
	if !stopped {
		t.Error("events stopped reaching the global log after a trace failure")
	}
}

func TestGlobalFailureNotifiesOnceAndKeepsGoing(t *testing.T) {
	var stderr bytes.Buffer
	global := &failingWriter{}

	logger := New(Options{Clock: clock.NewFake(base), Global: global, Stderr: &stderr})

	for i := 0; i < 5; i++ {
		logger.Info(EventSessionStarted, nil)
	}

	if global.writes != 1 {
		t.Errorf("global was written %d times after failing; want 1", global.writes)
	}
	if count := strings.Count(stderr.String(), "awake:"); count != 1 {
		t.Errorf("stderr got %d notices, want exactly 1:\n%s", count, stderr.String())
	}
	if !strings.Contains(stderr.String(), "session is unaffected") {
		t.Errorf("the notice does not reassure the user: %s", stderr.String())
	}
}

func TestLoggingSurvivesEverythingBroken(t *testing.T) {
	logger := New(Options{Clock: clock.NewFake(base), Global: &failingWriter{}})
	sessionLogger := logger.WithSession("session-id", &failingWriter{})

	// No panic, no error return: the session runs on.
	sessionLogger.Info(EventSessionStarted, nil)
	sessionLogger.Error(EventSessionFailed, Fields{"end_reason": "mode_failure", "elapsed": "0s"})
}

// --verbose changes where events are echoed, never what is logged.
func TestVerboseDoesNotChangeTheFiles(t *testing.T) {
	var withGlobal, withoutGlobal, verbose bytes.Buffer

	quiet := New(Options{Clock: clock.NewFake(base), Global: &withoutGlobal})
	loud := New(Options{Clock: clock.NewFake(base), Global: &withGlobal, Verbose: &verbose})

	for _, logger := range []*Logger{quiet, loud} {
		logger.Info(EventAppStarted, Fields{"app_version": "0.1.0", "command": "version"})
	}

	if withGlobal.String() != withoutGlobal.String() {
		t.Errorf("--verbose changed the log file:\n with: %s\nwithout: %s",
			withGlobal.String(), withoutGlobal.String())
	}
	if verbose.Len() == 0 {
		t.Error("--verbose produced no output")
	}
	if strings.HasPrefix(strings.TrimSpace(verbose.String()), "{") {
		t.Error("--verbose emitted raw JSON; it should be rendered for a human")
	}
	if !strings.Contains(verbose.String(), EventAppStarted) {
		t.Errorf("verbose output does not name the event: %s", verbose.String())
	}
}

// The global log has multiple writers. Whole-line appends must never interleave
// into a corrupt line.
func TestConcurrentWritesProduceWholeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "awake.jsonl")

	const writers, perWriter = 8, 50

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Each goroutine opens the file independently, as separate
			// processes would.
			file, err := OpenFile(path)
			if err != nil {
				t.Errorf("OpenFile() error = %v", err)
				return
			}
			defer file.Close()

			logger := New(Options{Clock: clock.NewFake(base), Global: file})
			for j := 0; j < perWriter; j++ {
				logger.Info(EventSessionStarted, Fields{"n": j})
			}
		}()
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	events := decodeLines(t, string(data))
	if len(events) != writers*perWriter {
		t.Errorf("got %d events, want %d", len(events), writers*perWriter)
	}
}

func TestOpenFileCreatesDirectoryAndPermissions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "logs", "sessions", "some-session.jsonl")

	file, err := OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != filePerm {
		t.Errorf("file mode = %04o, want %04o", got, filePerm)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != dirPerm {
		t.Errorf("dir mode = %04o, want %04o", got, dirPerm)
	}
}

func TestOpenFileAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "awake.jsonl")

	for i := 0; i < 3; i++ {
		file, err := OpenFile(path)
		if err != nil {
			t.Fatalf("OpenFile() error = %v", err)
		}
		New(Options{Clock: clock.NewFake(base), Global: file}).
			Info(EventSessionStarted, Fields{"run": i})
		file.Close()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if got := len(decodeLines(t, string(data))); got != 3 {
		t.Errorf("got %d events, want 3; reopening truncated the log", got)
	}
}
