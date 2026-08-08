package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javiervargas02/awake/internal/clock"
	"github.com/javiervargas02/awake/internal/lock"
	"github.com/javiervargas02/awake/internal/logging"
	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/session"
	"github.com/javiervargas02/awake/internal/store"
	"github.com/javiervargas02/awake/internal/update"
)

var base = time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)

// harness is one fully wired Awake, with a real filesystem in a temporary
// directory and fakes for time, the platform and the lock. Everything
// interesting here is about how the pieces fit together, so only the parts
// that cannot be exercised honestly in a test are faked.
type harness struct {
	service  *Service
	clock    *clock.Fake
	store    *store.Store
	platform *platform.Fake
	lock     *lock.Fake
	global   *bytes.Buffer
	root     string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	root := filepath.Join(t.TempDir(), ".awake")
	st := store.New(root)
	fakeClock := clock.NewFake(base)
	fakePlatform := platform.NewFake()
	fakeLock := lock.NewFake()

	var global bytes.Buffer
	logger := logging.New(logging.Options{Clock: fakeClock, Global: &global})

	return &harness{
		service: New(Deps{
			Clock:      fakeClock,
			Store:      st,
			Logger:     logger,
			Platform:   fakePlatform,
			Lock:       fakeLock,
			AppVersion: "0.1.0-test",
		}),
		clock:    fakeClock,
		store:    st,
		platform: fakePlatform,
		lock:     fakeLock,
		global:   &global,
		root:     root,
	}
}

// events returns the global log, decoded.
func (h *harness) events(t *testing.T) []map[string]any {
	t.Helper()

	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.global.String()), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("log line is not valid JSON: %v\n%s", err, line)
		}
		events = append(events, event)
	}
	return events
}

func (h *harness) eventNames(t *testing.T) []string {
	t.Helper()

	var names []string
	for _, event := range h.events(t) {
		names = append(names, event["event"].(string))
	}
	return names
}

func (h *harness) trace(t *testing.T, id session.ID) []map[string]any {
	t.Helper()

	data, err := os.ReadFile(h.store.SessionLogPath(id.String()))
	if err != nil {
		t.Fatalf("reading session trace: %v", err)
	}

	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("trace line is not valid JSON: %v\n%s", err, line)
		}
		events = append(events, event)
	}
	return events
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestSessionCompletesAtItsDeadline(t *testing.T) {
	h := newHarness(t)

	running, err := h.service.Start(context.Background(), StartRequest{Duration: 30 * time.Minute})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if h.platform.Running() != 1 {
		t.Fatal("the machine is not being kept awake")
	}

	done := make(chan *session.Session, 1)
	go func() {
		final, waitErr := running.Wait(context.Background())
		if waitErr != nil {
			t.Errorf("Wait() error = %v", waitErr)
		}
		done <- final
	}()

	h.clock.Advance(30 * time.Minute)

	select {
	case final := <-done:
		if final.Status != session.StatusCompleted {
			t.Errorf("status = %q, want %q", final.Status, session.StatusCompleted)
		}
		if final.EndReason != session.ReasonDurationElapsed {
			t.Errorf("end reason = %q, want %q", final.EndReason, session.ReasonDurationElapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the session never ended")
	}

	if h.platform.Running() != 0 {
		t.Error("the mechanism outlived the session")
	}
	if h.lock.Held() {
		t.Error("the exclusivity lock was not released")
	}
}

// Given only a session's trace, a reader must be able to reconstruct what
// happened, in order.
func TestTraceExplainsTheWholeSession(t *testing.T) {
	h := newHarness(t)

	running, err := h.service.Start(context.Background(), StartRequest{Duration: 30 * time.Minute})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	go h.clock.Advance(30 * time.Minute)
	if _, err := running.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	var names []string
	for _, event := range h.trace(t, running.Session.ID) {
		names = append(names, event["event"].(string))

		if event["session_id"] != running.Session.ID.String() {
			t.Errorf("trace contains an event for another session: %v", event)
		}
	}

	want := []string{
		logging.EventSessionCreated,
		logging.EventModeStarted,
		logging.EventSessionStarted,
		logging.EventModeStopped,
		logging.EventSessionCompleted,
	}
	if len(names) != len(want) {
		t.Fatalf("trace has %d events, want %d: %v", len(names), len(want), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("trace[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestStoppedSessionRecordsTheRightReason(t *testing.T) {
	cases := []struct {
		name  string
		cause error
		want  session.EndReason
	}{
		{"explicit stop", ErrStopRequested, session.ReasonUserStopped},
		{"interrupt", ErrInterrupted, session.ReasonInterrupted},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			ctx, cancel := context.WithCancelCause(context.Background())
			running, err := h.service.Start(ctx, StartRequest{Duration: time.Hour})
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}

			go cancel(tc.cause)

			final, err := running.Wait(ctx)
			if err != nil {
				t.Fatalf("Wait() error = %v", err)
			}
			if final.Status != session.StatusStopped {
				t.Errorf("status = %q, want %q", final.Status, session.StatusStopped)
			}
			if final.EndReason != tc.want {
				t.Errorf("end reason = %q, want %q", final.EndReason, tc.want)
			}
			if h.platform.Running() != 0 {
				t.Error("the mechanism outlived the session")
			}
		})
	}
}

// ADR-0008: acquisition is atomic, so a second start cannot conclude the coast
// is clear.
func TestSecondSessionIsRefused(t *testing.T) {
	h := newHarness(t)

	running, err := h.service.Start(context.Background(), StartRequest{Duration: time.Hour})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err = h.service.Start(context.Background(), StartRequest{Duration: time.Hour})
	if !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("second Start() error = %v, want ErrSessionRunning", err)
	}
	if !contains(h.eventNames(t), logging.EventSessionStartRefused) {
		t.Error("a refused start was not logged; a user who wonders why nothing happened deserves a record")
	}
	if h.platform.Starts() != 1 {
		t.Errorf("the mechanism was started %d times, want 1", h.platform.Starts())
	}

	go h.clock.Advance(time.Hour)
	if _, err := running.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

// Deleting ~/.awake mid-session must not permit a second session, because the
// lock does not live there.
func TestDeletingStateDoesNotPermitASecondSession(t *testing.T) {
	h := newHarness(t)

	running, err := h.service.Start(context.Background(), StartRequest{Duration: time.Hour})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := os.RemoveAll(h.root); err != nil {
		t.Fatalf("removing state: %v", err)
	}

	if _, err := h.service.Start(context.Background(), StartRequest{Duration: time.Hour}); !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("Start() after deleting state = %v, want ErrSessionRunning", err)
	}

	go h.clock.Advance(time.Hour)
	final, err := running.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if final.Status != session.StatusCompleted {
		t.Errorf("status = %q, want %q; the session should survive losing its state", final.Status, session.StatusCompleted)
	}

	// The record is recreated on the way out.
	if _, err := h.store.ReadSession(); err != nil {
		t.Errorf("session record was not rewritten after state loss: %v", err)
	}
}

func TestModeFailureFailsTheSession(t *testing.T) {
	h := newHarness(t)
	h.platform.StartErr = errors.New("mechanism refused to start")

	_, err := h.service.Start(context.Background(), StartRequest{Duration: time.Hour})
	if err == nil {
		t.Fatal("Start() succeeded with a broken mechanism")
	}

	record, readErr := h.store.ReadSession()
	if readErr != nil {
		t.Fatalf("ReadSession() error = %v", readErr)
	}
	if record.Status != session.StatusFailed || record.EndReason != session.ReasonModeFailure {
		t.Errorf("record = %q/%q, want failed/mode_failure", record.Status, record.EndReason)
	}
	if h.lock.Held() {
		t.Error("a failed start kept the lock")
	}

	names := h.eventNames(t)
	if !contains(names, logging.EventModeFailed) || !contains(names, logging.EventSessionFailed) {
		t.Errorf("failure was not fully logged: %v", names)
	}
}

// Awake never reports a session as running while failing to keep the machine
// awake, and it does not silently restart a dead mechanism.
func TestMechanismDeathEndsTheSession(t *testing.T) {
	h := newHarness(t)

	running, err := h.service.Start(context.Background(), StartRequest{Duration: time.Hour})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	h.platform.LastHandle().SimulateDeath(errors.New("mechanism exited unexpectedly"))

	final, err := running.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if final.Status != session.StatusFailed || final.EndReason != session.ReasonModeFailure {
		t.Errorf("session = %q/%q, want failed/mode_failure", final.Status, final.EndReason)
	}
	if h.platform.Starts() != 1 {
		t.Errorf("the mechanism was restarted; Starts() = %d", h.platform.Starts())
	}
}

// A machine that sleeps through its deadline wakes to find the session over.
// It is not extended to compensate, and the lateness is recorded.
func TestSleepingPastTheDeadlineRecordsOverrun(t *testing.T) {
	h := newHarness(t)

	running, err := h.service.Start(context.Background(), StartRequest{Duration: 30 * time.Minute})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	go h.clock.Advance(2 * time.Hour) // suspended, then woken

	final, err := running.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if final.Status != session.StatusCompleted {
		t.Errorf("status = %q, want %q", final.Status, session.StatusCompleted)
	}

	for _, event := range h.trace(t, final.ID) {
		if event["event"] != logging.EventSessionCompleted {
			continue
		}
		data := event["data"].(map[string]any)
		if data["overrun"] == "0s" {
			t.Error("a late ending was not recorded as late; it would look suspicious rather than explained")
		}
	}
}

func TestIndefiniteSessionRunsUntilStopped(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithCancelCause(context.Background())
	running, err := h.service.Start(ctx, StartRequest{Indefinite: true})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if running.Session.Deadline != nil {
		t.Error("an indefinite session has a deadline")
	}

	// Time passing changes nothing.
	h.clock.Advance(72 * time.Hour)

	select {
	case <-running.done:
		t.Fatal("an indefinite session ended on its own")
	default:
	}

	go cancel(ErrStopRequested)
	final, err := running.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if final.EndReason != session.ReasonUserStopped {
		t.Errorf("end reason = %q, want %q", final.EndReason, session.ReasonUserStopped)
	}
}

func TestStatusReportsRunningSession(t *testing.T) {
	h := newHarness(t)

	running, err := h.service.Start(context.Background(), StartRequest{Duration: 30 * time.Minute})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	h.clock.Advance(10 * time.Minute)

	result, err := h.service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !result.Running {
		t.Fatal("Status() says nothing is running during a session")
	}
	if result.Remaining == nil || *result.Remaining != 20*time.Minute {
		t.Errorf("remaining = %v, want 20m", result.Remaining)
	}

	go h.clock.Advance(20 * time.Minute)
	if _, err := running.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestStatusWithNoHistoryIsNotAnError(t *testing.T) {
	h := newHarness(t)

	result, err := h.service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v; an empty machine is a normal state", err)
	}
	if result.Session != nil || result.Running {
		t.Errorf("result = %+v, want an empty result", result)
	}
}

func TestStatusReportsIndefiniteSessionWithoutRemaining(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithCancelCause(context.Background())
	running, err := h.service.Start(ctx, StartRequest{Indefinite: true})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	result, err := h.service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !result.Running {
		t.Fatal("Status() says nothing is running")
	}
	if result.Remaining != nil {
		t.Errorf("remaining = %v, want nil for an indefinite session", *result.Remaining)
	}

	go cancel(ErrStopRequested)
	running.Wait(ctx)
}

// A session whose owner vanished can never write its own ending, so the next
// command resolves it.
func TestCrashedSessionIsRecovered(t *testing.T) {
	h := newHarness(t)

	// A session that was running when its process died: a non-terminal record
	// with a mechanism PID, and a free lock.
	crashed, err := session.New(base, session.Params{
		Mode:       session.ModeSystem,
		Duration:   time.Hour,
		AppVersion: "0.1.0-test",
		OwnerPID:   999999,
	})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}
	crashed.MechanismPID = 4242
	if err := h.store.WriteSession(crashed); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	h.platform.ReclaimResult = true
	h.clock.Advance(90 * time.Minute)

	result, err := h.service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if result.Running {
		t.Error("Status() reported a crashed session as running")
	}
	if result.Session.Status != session.StatusFailed || result.Session.EndReason != session.ReasonCrashed {
		t.Errorf("recovered session = %q/%q, want failed/crashed",
			result.Session.Status, result.Session.EndReason)
	}
	if got := h.platform.Reclaimed(); len(got) != 1 || got[0] != 4242 {
		t.Errorf("Reclaimed() = %v, want [4242]", got)
	}
	if !contains(h.eventNames(t), logging.EventSessionRecovered) {
		t.Error("recovery was not logged")
	}

	// The end time is the moment of discovery, not an invented time of death.
	if !result.Session.EndedAt.Equal(base.Add(90 * time.Minute)) {
		t.Errorf("ended_at = %v, want the moment of discovery", result.Session.EndedAt)
	}
}

func TestStartRecoversBeforeStartingAnew(t *testing.T) {
	h := newHarness(t)

	crashed, err := session.New(base, session.Params{
		Mode: session.ModeSystem, Duration: time.Hour, OwnerPID: 999999,
	})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}
	crashed.MechanismPID = 4242
	if err := h.store.WriteSession(crashed); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}

	// Recovery is never a command the user has to run first.
	running, err := h.service.Start(context.Background(), StartRequest{Duration: time.Hour})
	if err != nil {
		t.Fatalf("Start() after a crash error = %v", err)
	}
	if running.Session.ID == crashed.ID {
		t.Error("the new session reused the crashed session's identity")
	}
	if got := h.platform.Reclaimed(); len(got) != 1 || got[0] != 4242 {
		t.Errorf("Reclaimed() = %v, want [4242]", got)
	}

	go h.clock.Advance(time.Hour)
	running.Wait(context.Background())
}

func TestStopWithNoSession(t *testing.T) {
	h := newHarness(t)

	if _, err := h.service.Stop(context.Background()); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Stop() error = %v, want ErrNoSession", err)
	}
}

// A config with a bad value degrades that key and never blocks a session.
func TestBadConfigDoesNotBlockASession(t *testing.T) {
	h := newHarness(t)

	if err := h.store.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := os.WriteFile(h.store.ConfigPath(),
		[]byte("[session]\ndefault_duration = \"not a duration\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	running, err := h.service.Start(context.Background(), StartRequest{})
	if err != nil {
		t.Fatalf("Start() error = %v; a bad config must not stop a session", err)
	}
	if want := 30 * time.Minute; running.Session.RequestedDuration.Std() != want {
		t.Errorf("duration = %v, want the default %v", running.Session.RequestedDuration.Std(), want)
	}
	if !contains(h.eventNames(t), logging.EventConfigDefaulted) {
		t.Error("the fallback was not logged")
	}

	go h.clock.Advance(30 * time.Minute)
	running.Wait(context.Background())
}

// A machine that cannot verify assertions still runs sessions; it says so.
func TestUnverifiableAssertionStillRuns(t *testing.T) {
	h := newHarness(t)
	h.platform.VerificationResult = platform.Unverifiable

	running, err := h.service.Start(context.Background(), StartRequest{Duration: time.Hour})
	if err != nil {
		t.Fatalf("Start() error = %v; an unverifiable platform must still run", err)
	}
	if running.Verification != platform.Unverifiable {
		t.Errorf("verification = %q, want %q", running.Verification, platform.Unverifiable)
	}

	for _, event := range h.trace(t, running.Session.ID) {
		if event["event"] != logging.EventModeStarted {
			continue
		}
		if event["level"] != string(logging.LevelWarn) {
			t.Errorf("an unverified session was logged at %v, want warn", event["level"])
		}
		data := event["data"].(map[string]any)
		if data["assertion_verified"] != string(platform.Unverifiable) {
			t.Errorf("assertion_verified = %v", data["assertion_verified"])
		}
	}

	go h.clock.Advance(time.Hour)
	running.Wait(context.Background())
}

// If the lock cannot be consulted at all, Awake degrades rather than refusing.
func TestUnavailableLockDegradesRatherThanRefusing(t *testing.T) {
	h := newHarness(t)
	h.lock.Err = errors.New("temp directory is not writable")

	running, err := h.service.Start(context.Background(), StartRequest{Duration: time.Hour})
	if err != nil {
		t.Fatalf("Start() error = %v; a broken lock must not stop a session", err)
	}

	var sawWarning bool
	for _, event := range h.events(t) {
		if event["event"] != logging.EventSessionStartRefused {
			continue
		}
		if event["data"].(map[string]any)["reason"] == "lock_unavailable" {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Error("degraded exclusivity was not reported")
	}

	go h.clock.Advance(time.Hour)
	running.Wait(context.Background())
}

func TestWaitIsIdempotent(t *testing.T) {
	h := newHarness(t)

	running, err := h.service.Start(context.Background(), StartRequest{Duration: time.Minute})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	go h.clock.Advance(time.Minute)

	first, err := running.Wait(context.Background())
	if err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}
	second, err := running.Wait(context.Background())
	if err != nil {
		t.Fatalf("second Wait() error = %v", err)
	}
	if first != second {
		t.Error("a second Wait() produced a different result")
	}
	if h.lock.Releases() != 1 {
		t.Errorf("the lock was released %d times, want 1", h.lock.Releases())
	}
}

func TestInvalidRequestsAreRejected(t *testing.T) {
	cases := []struct {
		name string
		req  StartRequest
	}{
		{"negative duration", StartRequest{Duration: -time.Minute}},
		{"duration with indefinite", StartRequest{Duration: time.Hour, Indefinite: true}},
		{"unknown mode", StartRequest{Mode: "telepathy", Duration: time.Hour}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)

			if _, err := h.service.Start(context.Background(), tc.req); err == nil {
				t.Fatal("Start() accepted an invalid request")
			}
			if h.lock.Held() {
				t.Error("a rejected request kept the lock")
			}
			if h.platform.Starts() != 0 {
				t.Error("a rejected request started the mechanism")
			}
		})
	}
}

// serveManifest points the service at a local test server and reports whether
// it was ever contacted.
func (h *harness) serveManifest(t *testing.T, body string) *int32 {
	t.Helper()

	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)

	h.service.checker = &update.Checker{URL: server.URL, Client: server.Client()}
	return &hits
}

const testManifest = `{"schema_version":1,"channels":{"stable":{"version":"0.2.0",
	"severity":"recommended","notes_url":"https://example.invalid/v0.2.0"}}}`

func TestUpdateCheckReportsAndCaches(t *testing.T) {
	h := newHarness(t)
	hits := h.serveManifest(t, testManifest)

	result, err := h.service.CheckUpdate(context.Background(), false)
	if err != nil {
		t.Fatalf("CheckUpdate() error = %v", err)
	}
	if !result.Available() {
		t.Fatalf("outcome = %q, want an available update (err: %v)", result.Outcome, result.Err)
	}
	if atomic.LoadInt32(hits) != 1 {
		t.Errorf("the host was contacted %d times, want 1", atomic.LoadInt32(hits))
	}

	names := h.eventNames(t)
	for _, want := range []string{
		logging.EventUpdateCheckStarted,
		logging.EventUpdateCheckCompleted,
		logging.EventUpdateAvailable,
	} {
		if !contains(names, want) {
			t.Errorf("missing event %q in %v", want, names)
		}
	}

	// A second check inside the interval must not touch the network.
	second, err := h.service.CheckUpdate(context.Background(), false)
	if err != nil {
		t.Fatalf("second CheckUpdate() error = %v", err)
	}
	if atomic.LoadInt32(hits) != 1 {
		t.Errorf("a cached answer still contacted the host (%d hits)", atomic.LoadInt32(hits))
	}
	if !second.FromCache {
		t.Error("the second result did not report itself as cached")
	}

	// --force ignores the cache.
	if _, err := h.service.CheckUpdate(context.Background(), true); err != nil {
		t.Fatalf("forced CheckUpdate() error = %v", err)
	}
	if atomic.LoadInt32(hits) != 2 {
		t.Errorf("--force did not re-check (%d hits)", atomic.LoadInt32(hits))
	}
}

// With checking disabled, Awake makes no network request of any kind. This is
// what makes principle 5's "the only network activity" claim verifiable.
func TestDisabledUpdatesMakeNoRequest(t *testing.T) {
	h := newHarness(t)
	hits := h.serveManifest(t, testManifest)

	if err := h.store.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := os.WriteFile(h.store.ConfigPath(),
		[]byte("[updates]\nenabled = false\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	result, err := h.service.CheckUpdate(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckUpdate() error = %v", err)
	}
	if result.Outcome != update.OutcomeDisabled {
		t.Errorf("outcome = %q, want %q", result.Outcome, update.OutcomeDisabled)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Errorf("the host was contacted %d times with updates disabled; it must be zero", got)
	}
}

// An unreachable host is a warning, never an error a command fails over.
func TestUpdateCheckFailureIsNotAnError(t *testing.T) {
	h := newHarness(t)

	server := httptest.NewServer(http.NotFoundHandler())
	client := server.Client()
	url := server.URL
	server.Close()
	h.service.checker = &update.Checker{URL: url, Client: client}

	result, err := h.service.CheckUpdate(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckUpdate() returned an error for an unreachable host: %v", err)
	}
	if result.Outcome != update.OutcomeFailed {
		t.Errorf("outcome = %q, want %q", result.Outcome, update.OutcomeFailed)
	}

	for _, event := range h.events(t) {
		if event["event"] == logging.EventUpdateCheckCompleted && event["level"] != "warn" {
			t.Errorf("a failed check logged at %v, want warn", event["level"])
		}
	}

	// The failure is cached, so an offline machine does not retry constantly.
	cached, ok := h.service.LastUpdateCheck()
	if !ok || cached.Result != update.OutcomeFailed {
		t.Errorf("the failure was not cached: %+v", cached)
	}
}
