package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javiervargas02/awake/internal/app"
	"github.com/javiervargas02/awake/internal/buildinfo"
	"github.com/javiervargas02/awake/internal/clock"
	"github.com/javiervargas02/awake/internal/lock"
	"github.com/javiervargas02/awake/internal/logging"
	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/store"
	"github.com/javiervargas02/awake/internal/update"
)

var base = time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)

// harness is a wired CLI over a real filesystem in a temporary directory, with
// fakes only for the things that cannot be exercised honestly in a test: the
// clock, the platform and the lock.
type harness struct {
	deps     Deps
	service  *app.Service
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	clock    *clock.Fake
	platform *platform.Fake
	store    *store.Store
	lock     *lock.Fake
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	st := store.New(filepath.Join(t.TempDir(), ".awake"))
	fakeClock := clock.NewFake(base)
	fakePlatform := platform.NewFake()
	fakeLock := lock.NewFake()

	var stdout, stderr, global bytes.Buffer

	h := &harness{
		stdout:   &stdout,
		stderr:   &stderr,
		clock:    fakeClock,
		platform: fakePlatform,
		store:    st,
		lock:     fakeLock,
	}

	service := app.New(app.Deps{
		Clock:      fakeClock,
		Store:      st,
		Logger:     logging.New(logging.Options{Clock: fakeClock, Global: &global}),
		Platform:   fakePlatform,
		Lock:       fakeLock,
		AppVersion: "0.1.0-test",
	})

	h.service = service
	h.deps = Deps{
		Stdout: &stdout,
		Stderr: &stderr,
		Version: buildinfo.Info{
			Version: "0.1.0-test", Commit: "abc1234", Built: "2026-08-07T14:00:00Z",
			GoVersion: "go1.26.4", Platform: "darwin/arm64",
		},
		NewService: func(bool) (*app.Service, func(), error) {
			return service, func() {}, nil
		},
	}

	return h
}

func (h *harness) run(args ...string) int {
	return Run(context.Background(), args, h.deps)
}

func decodeJSON(t *testing.T, raw string) map[string]any {
	t.Helper()

	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, raw)
	}
	return value
}

func TestExitCodes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no arguments prints help", nil, ExitOK},
		{"help flag", []string{"--help"}, ExitOK},
		{"version", []string{"version"}, ExitOK},
		{"version json", []string{"version", "--json"}, ExitOK},
		{"status with no history", []string{"status"}, ExitOK},
		{"unknown command", []string{"nope"}, ExitUsage},
		{"unknown flag", []string{"--nope"}, ExitUsage},
		{"unknown flag on command", []string{"version", "--nope"}, ExitUsage},
		{"unexpected argument", []string{"version", "extra"}, ExitUsage},
		{"malformed duration", []string{"start", "half an hour"}, ExitUsage},
		{"negative duration", []string{"start", "-5m"}, ExitUsage},
		{"zero duration", []string{"start", "0s"}, ExitUsage},
		{"duration with indefinite", []string{"start", "30m", "--indefinite"}, ExitUsage},
		{"too many arguments", []string{"start", "30m", "45m"}, ExitUsage},
		{"stop with nothing running", []string{"stop"}, ExitPrecondition},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if got := h.run(tc.args...); got != tc.want {
				t.Errorf("Run(%q) = %d, want %d (stderr: %s)",
					tc.args, got, tc.want, h.stderr.String())
			}
		})
	}
}

// Stdout is a data channel: a failed command must not write anything a script
// would have to filter out.
func TestErrorsGoToStderr(t *testing.T) {
	h := newHarness(t)

	if code := h.run("nope"); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
	if h.stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", h.stdout.String())
	}
	if !strings.Contains(h.stderr.String(), "unknown command") {
		t.Errorf("stderr = %q, want it to explain the error", h.stderr.String())
	}
}

func TestVersionJSONShape(t *testing.T) {
	h := newHarness(t)

	if code := h.run("version", "--json"); code != ExitOK {
		t.Fatalf("exit code = %d (stderr: %s)", code, h.stderr.String())
	}

	got := decodeJSON(t, h.stdout.String())
	want := []string{"schema_version", "version", "commit", "built", "go_version", "platform"}

	for _, field := range want {
		if _, ok := got[field]; !ok {
			t.Errorf("missing field %q in %v", field, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d fields, want %d: %v", len(got), len(want), got)
	}
}

// `awake version` must not create state: installing Awake and asking its
// version should leave no trace.
func TestVersionCreatesNothing(t *testing.T) {
	h := newHarness(t)
	h.deps.NewService = func(bool) (*app.Service, func(), error) {
		t.Fatal("version built the application service")
		return nil, nil, nil
	}

	if code := h.run("version"); code != ExitOK {
		t.Fatalf("exit code = %d", code)
	}
	if store.Exists(h.store.Root()) {
		t.Error("version created the state directory")
	}
}

func TestStatusJSONShapeWithNoHistory(t *testing.T) {
	h := newHarness(t)

	if code := h.run("status", "--json"); code != ExitOK {
		t.Fatalf("exit code = %d (stderr: %s)", code, h.stderr.String())
	}

	got := decodeJSON(t, h.stdout.String())
	if got["running"] != false {
		t.Errorf("running = %v, want false", got["running"])
	}
	if got["session"] != nil {
		t.Errorf("session = %v, want null when nothing has run", got["session"])
	}
}

func TestStatusWithNoHistoryIsFriendly(t *testing.T) {
	h := newHarness(t)

	if code := h.run("status"); code != ExitOK {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(h.stdout.String(), "No sessions have run") {
		t.Errorf("stdout = %q", h.stdout.String())
	}
}

// A full session through the CLI: start, run to the deadline, report.
func TestStartRunsAndReportsCompletion(t *testing.T) {
	h := newHarness(t)

	done := make(chan int, 1)
	go func() { done <- h.run("start", "30m") }()

	// Wait for the session to be recorded, then let its deadline arrive.
	waitFor(t, func() bool {
		_, err := h.store.ReadSession()
		return err == nil
	})
	h.clock.Advance(30 * time.Minute)

	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("exit code = %d (stderr: %s)", code, h.stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start never returned")
	}

	out := h.stdout.String()
	if !strings.Contains(out, "Keeping this computer awake") {
		t.Errorf("no start message:\n%s", out)
	}
	if !strings.Contains(out, "Session completed") {
		t.Errorf("no completion message:\n%s", out)
	}
	if h.platform.Running() != 0 {
		t.Error("the mechanism outlived the command")
	}
}

// --json emits JSON Lines: one object when the session starts, one when it
// ends, so a script can act on the start without waiting for the end.
func TestStartJSONIsLineDelimited(t *testing.T) {
	h := newHarness(t)

	done := make(chan int, 1)
	go func() { done <- h.run("start", "30m", "--json") }()

	waitFor(t, func() bool {
		_, err := h.store.ReadSession()
		return err == nil
	})
	h.clock.Advance(30 * time.Minute)

	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("exit code = %d (stderr: %s)", code, h.stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start never returned")
	}

	// Indented objects, so split on the decoder rather than on newlines.
	decoder := json.NewDecoder(strings.NewReader(h.stdout.String()))

	var events []map[string]any
	for decoder.More() {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("output is not a stream of JSON objects: %v\n%s", err, h.stdout.String())
		}
		events = append(events, event)
	}

	if len(events) != 2 {
		t.Fatalf("got %d JSON objects, want 2 (started, ended):\n%s", len(events), h.stdout.String())
	}
	if events[0]["event"] != "started" || events[1]["event"] != "ended" {
		t.Errorf("events = %v, %v; want started, ended", events[0]["event"], events[1]["event"])
	}

	session, ok := events[1]["session"].(map[string]any)
	if !ok {
		t.Fatalf("ended event has no session: %v", events[1])
	}
	if session["status"] != "completed" {
		t.Errorf("status = %v, want completed", session["status"])
	}
	if session["remaining"] != nil {
		t.Errorf("remaining = %v, want null for an ended session", session["remaining"])
	}
}

// An indefinite session reports no scheduled end rather than inventing one.
func TestIndefiniteSessionReportsNoEnd(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"start", "--indefinite", "--json"}, h.deps) }()

	waitFor(t, func() bool {
		_, err := h.store.ReadSession()
		return err == nil
	})
	cancel(app.ErrStopRequested)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("start never returned")
	}

	decoder := json.NewDecoder(strings.NewReader(h.stdout.String()))
	var started map[string]any
	if err := decoder.Decode(&started); err != nil {
		t.Fatalf("decoding first object: %v", err)
	}

	session := started["session"].(map[string]any)
	if session["indefinite"] != true {
		t.Errorf("indefinite = %v, want true", session["indefinite"])
	}
	if session["deadline"] != nil {
		t.Errorf("deadline = %v, want null", session["deadline"])
	}
	if session["remaining"] != nil {
		t.Errorf("remaining = %v, want null rather than a sentinel", session["remaining"])
	}
}

func TestSecondStartIsRefused(t *testing.T) {
	h := newHarness(t)

	done := make(chan int, 1)
	go func() { done <- h.run("start", "30m") }()

	waitFor(t, func() bool {
		_, err := h.store.ReadSession()
		return err == nil
	})

	second := newHarnessSharing(t, h)
	if code := second.run("start", "30m"); code != ExitPrecondition {
		t.Errorf("second start = %d, want %d (stderr: %s)",
			code, ExitPrecondition, second.stderr.String())
	}

	h.clock.Advance(30 * time.Minute)
	<-done
}

// A running session is reported as running, with its remaining time.
func TestStatusDuringASession(t *testing.T) {
	h := newHarness(t)

	done := make(chan int, 1)
	go func() { done <- h.run("start", "30m") }()

	waitFor(t, func() bool {
		_, err := h.store.ReadSession()
		return err == nil
	})
	h.clock.Advance(10 * time.Minute)

	observer := newHarnessSharing(t, h)
	if code := observer.run("status", "--json"); code != ExitOK {
		t.Fatalf("status = %d (stderr: %s)", code, observer.stderr.String())
	}

	got := decodeJSON(t, observer.stdout.String())
	if got["running"] != true {
		t.Errorf("running = %v, want true", got["running"])
	}

	session := got["session"].(map[string]any)
	if session["remaining"] != "20m0s" {
		t.Errorf("remaining = %v, want 20m0s", session["remaining"])
	}
	if session["status"] != "running" {
		t.Errorf("status = %v, want running", session["status"])
	}

	h.clock.Advance(20 * time.Minute)
	<-done
}

func TestHelpMentionsEveryCommand(t *testing.T) {
	h := newHarness(t)

	if code := h.run("--help"); code != ExitOK {
		t.Fatalf("exit code = %d", code)
	}

	out := h.stdout.String()
	for _, cmd := range commands() {
		if !strings.Contains(out, cmd.name) {
			t.Errorf("help does not mention %q", cmd.name)
		}
	}
	if !strings.Contains(out, "not a stealth tool") {
		t.Error("help does not state what Awake is not")
	}
}

// newHarnessSharing returns a second CLI over the same state, as a second
// process would be — except that the lock is in-process, so it is shared too.
func newHarnessSharing(t *testing.T, first *harness) *harness {
	t.Helper()

	var stdout, stderr, global bytes.Buffer

	service := app.New(app.Deps{
		Clock:      first.clock,
		Store:      first.store,
		Logger:     logging.New(logging.Options{Clock: first.clock, Global: &global}),
		Platform:   first.platform,
		Lock:       first.lock,
		AppVersion: "0.1.0-test",
	})

	return &harness{
		stdout:   &stdout,
		stderr:   &stderr,
		clock:    first.clock,
		platform: first.platform,
		store:    first.store,
		lock:     first.lock,
		deps: Deps{
			Stdout:  &stdout,
			Stderr:  &stderr,
			Version: first.deps.Version,
			NewService: func(bool) (*app.Service, func(), error) {
				return service, func() {}, nil
			},
		},
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

// Go's flag package stops parsing at the first positional argument, which would
// make `awake start 30m --json` a usage error while `awake start --json 30m`
// worked. Users write the first form.
func TestFlagsAndArgumentsInAnyOrder(t *testing.T) {
	for _, args := range [][]string{
		{"start", "30m", "--json"},
		{"start", "--json", "30m"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)

			done := make(chan int, 1)
			go func() { done <- h.run(args...) }()

			waitFor(t, func() bool {
				_, err := h.store.ReadSession()
				return err == nil
			})
			h.clock.Advance(30 * time.Minute)

			select {
			case code := <-done:
				if code != ExitOK {
					t.Fatalf("exit code = %d, want %d (stderr: %s)",
						code, ExitOK, h.stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("start never returned")
			}

			if !strings.Contains(h.stdout.String(), `"event": "started"`) {
				t.Errorf("--json was not honoured:\n%s", h.stdout.String())
			}
		})
	}
}

func TestDoctorOnHealthyMachineExitsZero(t *testing.T) {
	h := newHarness(t)

	// A machine that has never run Awake is healthy, not broken.
	if code := h.run("doctor"); code != ExitOK {
		t.Fatalf("doctor = %d, want %d (stdout: %s)", code, ExitOK, h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "checks:") {
		t.Errorf("doctor printed no summary:\n%s", h.stdout.String())
	}
}

// Exit code 5 means problems were found; warnings alone still exit 0.
func TestDoctorExitsFiveOnProblems(t *testing.T) {
	h := newHarness(t)
	if err := h.store.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := os.WriteFile(h.store.SessionPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	if code := h.run("doctor"); code != ExitProblemsFound {
		t.Fatalf("doctor = %d, want %d (stdout: %s)", code, ExitProblemsFound, h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "PROBLEM") {
		t.Errorf("the problem was not shown:\n%s", h.stdout.String())
	}
	if !strings.Contains(h.stdout.String(), "awake repair") {
		t.Error("doctor did not name the command that fixes it")
	}
}

func TestDoctorJSONShape(t *testing.T) {
	h := newHarness(t)

	if code := h.run("doctor", "--json"); code != ExitOK {
		t.Fatalf("doctor = %d (stderr: %s)", code, h.stderr.String())
	}

	got := decodeJSON(t, h.stdout.String())
	for _, field := range []string{"schema_version", "healthy", "summary", "findings"} {
		if _, ok := got[field]; !ok {
			t.Errorf("missing field %q in %v", field, got)
		}
	}

	findings, ok := got["findings"].([]any)
	if !ok || len(findings) == 0 {
		t.Fatalf("findings = %v, want a non-empty list", got["findings"])
	}
	first := findings[0].(map[string]any)
	for _, field := range []string{"check", "status", "detail"} {
		if _, ok := first[field]; !ok {
			t.Errorf("finding missing %q: %v", field, first)
		}
	}
}

// Doctor is the dry run for repair: it never changes anything.
func TestDoctorChangesNothing(t *testing.T) {
	h := newHarness(t)
	if err := h.store.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := os.WriteFile(h.store.ConfigPath(), []byte("not [ toml"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	h.run("doctor")

	after, err := os.ReadFile(h.store.ConfigPath())
	if err != nil {
		t.Fatalf("doctor removed the config: %v", err)
	}
	if string(after) != "not [ toml" {
		t.Error("doctor modified the config")
	}
}

func TestRepairFixesAndIsIdempotent(t *testing.T) {
	h := newHarness(t)
	if err := h.store.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := os.WriteFile(h.store.SessionPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	if code := h.run("repair"); code != ExitOK {
		t.Fatalf("repair = %d (stderr: %s)", code, h.stderr.String())
	}
	if code := h.run("doctor"); code != ExitOK {
		t.Fatalf("doctor after repair = %d, want %d (stdout: %s)",
			code, ExitOK, h.stdout.String())
	}

	// A second repair has nothing left to do.
	h.stdout.Reset()
	if code := h.run("repair"); code != ExitOK {
		t.Fatalf("second repair = %d", code)
	}
}

// Repair leaves quarantined files in place and names the flag that deletes
// them: deleting a user's files is their decision.
func TestRepairKeepsQuarantineUntilAsked(t *testing.T) {
	h := newHarness(t)
	if err := h.store.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := os.WriteFile(h.store.SessionPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	h.run("repair")

	found, err := h.store.ListQuarantined()
	if err != nil {
		t.Fatalf("ListQuarantined() error = %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d quarantined files, want 1", len(found))
	}

	h.stdout.Reset()
	h.run("repair")
	if !strings.Contains(h.stdout.String(), "--clean-quarantine") {
		t.Errorf("repair did not name the flag that deletes them:\n%s", h.stdout.String())
	}

	if code := h.run("repair", "--clean-quarantine"); code != ExitOK {
		t.Fatalf("repair --clean-quarantine = %d", code)
	}
	found, err = h.store.ListQuarantined()
	if err != nil {
		t.Fatalf("ListQuarantined() error = %v", err)
	}
	if len(found) != 0 {
		t.Errorf("%d quarantined files survived --clean-quarantine", len(found))
	}
}

// withManifest points the harness at a local update host. No test touches the
// real network.
func (h *harness) withManifest(t *testing.T, body string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)

	var global bytes.Buffer
	service := app.New(app.Deps{
		Clock:      h.clock,
		Store:      h.store,
		Logger:     logging.New(logging.Options{Clock: h.clock, Global: &global}),
		Platform:   h.platform,
		Lock:       h.lock,
		AppVersion: "0.1.0",
		Checker:    &update.Checker{URL: server.URL, Client: server.Client()},
	})

	h.service = service
	h.deps.NewService = func(bool) (*app.Service, func(), error) {
		return service, func() {}, nil
	}
}

const availableManifest = `{
  "schema_version": 1,
  "channels": {
    "stable": {
      "version": "0.2.0",
      "severity": "recommended",
      "released": "2026-09-01",
      "notes_url": "https://github.com/javiervargas02/awake/releases/tag/v0.2.0"
    }
  }
}`

// When an update exists, Awake must say where to read about it and how to get
// it — it never installs anything itself (ADR-0005).
func TestUpdateAvailablePointsAtTheReleaseNotes(t *testing.T) {
	h := newHarness(t)
	h.withManifest(t, availableManifest)

	if code := h.run("update", "check"); code != ExitOK {
		t.Fatalf("update check = %d (stderr: %s)", code, h.stderr.String())
	}

	out := h.stdout.String()
	t.Logf("rendered output:\n%s", out)

	for _, want := range []string{
		"0.2.0",
		"https://github.com/javiervargas02/awake/releases/tag/v0.2.0",
		"does not install",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not mention %q:\n%s", want, out)
		}
	}
}

// A security release says so, prominently, before the link.
func TestSecurityReleaseIsCalledOut(t *testing.T) {
	h := newHarness(t)
	h.withManifest(t, `{"schema_version":1,"channels":{"stable":{
		"version":"0.2.1","severity":"security",
		"notes_url":"https://example.invalid/v0.2.1"}}}`)

	if code := h.run("update", "check"); code != ExitOK {
		t.Fatalf("update check = %d", code)
	}

	out := h.stdout.String()
	t.Logf("rendered output:\n%s", out)

	if !strings.Contains(out, "security release") {
		t.Errorf("a security release was not called out:\n%s", out)
	}
	if !strings.Contains(out, "https://example.invalid/v0.2.1") {
		t.Errorf("no release notes link:\n%s", out)
	}
}

// The notes URL is part of the machine-readable contract too, so a script or a
// future GUI can link to it without scraping prose.
func TestUpdateJSONCarriesTheNotesURL(t *testing.T) {
	h := newHarness(t)
	h.withManifest(t, availableManifest)

	if code := h.run("update", "check", "--json"); code != ExitOK {
		t.Fatalf("update check --json = %d (stderr: %s)", code, h.stderr.String())
	}

	got := decodeJSON(t, h.stdout.String())
	t.Logf("rendered output:\n%s", h.stdout.String())

	if got["result"] != "update_available" {
		t.Errorf("result = %v, want update_available", got["result"])
	}
	if got["notes_url"] != "https://github.com/javiervargas02/awake/releases/tag/v0.2.0" {
		t.Errorf("notes_url = %v", got["notes_url"])
	}
	if got["severity"] != "recommended" {
		t.Errorf("severity = %v", got["severity"])
	}
}

// A manifest without a notes URL must not print a dangling label.
func TestMissingNotesURLIsOmittedCleanly(t *testing.T) {
	h := newHarness(t)
	h.withManifest(t, `{"schema_version":1,"channels":{"stable":{"version":"0.2.0"}}}`)

	if code := h.run("update", "check"); code != ExitOK {
		t.Fatalf("update check = %d", code)
	}

	out := h.stdout.String()
	if strings.Contains(out, "Release notes:") {
		t.Errorf("printed an empty release-notes label:\n%s", out)
	}
	if !strings.Contains(out, "0.2.0") {
		t.Errorf("did not report the available version:\n%s", out)
	}
}
