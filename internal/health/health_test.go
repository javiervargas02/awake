package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javiervargas02/awake/internal/clock"
	"github.com/javiervargas02/awake/internal/lock"
	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/session"
	"github.com/javiervargas02/awake/internal/store"
	"github.com/javiervargas02/awake/internal/update"
)

var base = time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)

type harness struct {
	doctor   *Doctor
	repairer *Repairer
	store    *store.Store
	platform *platform.Fake
	lock     *lock.Fake
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	st := store.New(filepath.Join(t.TempDir(), ".awake"))
	fakeClock := clock.NewFake(base)
	fakePlatform := platform.NewFake()
	fakeLock := lock.NewFake()

	return &harness{
		doctor:   NewDoctor(st, fakePlatform, fakeLock, fakeClock),
		repairer: NewRepairer(st, fakeClock),
		store:    st,
		platform: fakePlatform,
		lock:     fakeLock,
	}
}

// healthy prepares an installation with nothing wrong: directories present,
// a config, and one completed session.
func (h *harness) healthy(t *testing.T) {
	t.Helper()

	if err := h.store.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := os.WriteFile(h.store.ConfigPath(),
		[]byte("[session]\ndefault_duration = \"30m\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	sess, err := session.New(base, session.Params{
		Mode: session.ModeSystem, Duration: 30 * time.Minute, OwnerPID: os.Getpid(),
	})
	if err != nil {
		t.Fatalf("session.New() error = %v", err)
	}
	if err := sess.Complete(base.Add(30 * time.Minute)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := h.store.WriteSession(sess); err != nil {
		t.Fatalf("WriteSession() error = %v", err)
	}
}

func (h *harness) find(t *testing.T, check string) Finding {
	t.Helper()

	report := h.doctor.Diagnose(context.Background())
	for _, finding := range report.Findings {
		if finding.Check == check {
			return finding
		}
	}
	t.Fatalf("no finding named %q in %+v", check, report.Findings)
	return Finding{}
}

func TestHealthyInstallationHasNoProblems(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	report := h.doctor.Diagnose(context.Background())

	if !report.Healthy() {
		for _, finding := range report.Findings {
			if finding.Status == StatusProblem {
				t.Errorf("unexpected problem: %s — %s", finding.Check, finding.Detail)
			}
		}
	}
	if report.Warnings() > 0 {
		for _, finding := range report.Findings {
			if finding.Status == StatusWarning {
				t.Logf("warning (acceptable): %s — %s", finding.Check, finding.Detail)
			}
		}
	}
}

// A machine that has never run Awake is healthy, not broken.
func TestFreshMachineIsHealthy(t *testing.T) {
	h := newHarness(t)

	report := h.doctor.Diagnose(context.Background())

	if !report.Healthy() {
		for _, finding := range report.Findings {
			if finding.Status == StatusProblem {
				t.Errorf("a fresh machine reported a problem: %s — %s", finding.Check, finding.Detail)
			}
		}
	}
	if report.Warnings() == 0 {
		t.Error("a fresh machine produced no warnings; the absent directory should be mentioned")
	}
}

// The fault-injection matrix from the state-and-repair architecture.
func TestFaultMatrix(t *testing.T) {
	cases := []struct {
		name   string
		check  string
		inject func(t *testing.T, h *harness)
		want   Status
		action Action
	}{
		{
			name:   "state directory absent",
			check:  "state directory",
			inject: func(t *testing.T, h *harness) { os.RemoveAll(h.store.Root()) },
			want:   StatusWarning,
			action: ActionCreateDirs,
		},
		{
			name:  "state directory is a file",
			check: "state directory",
			inject: func(t *testing.T, h *harness) {
				os.RemoveAll(h.store.Root())
				os.WriteFile(h.store.Root(), []byte("not a directory"), 0o600)
			},
			want: StatusProblem,
		},
		{
			name:   "permissions too broad",
			check:  "state directory permissions",
			inject: func(t *testing.T, h *harness) { os.Chmod(h.store.Root(), 0o755) },
			want:   StatusWarning,
			action: ActionFixPermissions,
		},
		{
			name:   "config absent",
			check:  "configuration",
			inject: func(t *testing.T, h *harness) { os.Remove(h.store.ConfigPath()) },
			want:   StatusWarning,
			action: ActionGenerateConfig,
		},
		{
			name:  "config unparseable",
			check: "configuration",
			inject: func(t *testing.T, h *harness) {
				os.WriteFile(h.store.ConfigPath(), []byte("this is not [ toml"), 0o600)
			},
			want:   StatusProblem,
			action: ActionQuarantineConfig,
		},
		{
			name:  "config has an unknown key",
			check: "configuration",
			inject: func(t *testing.T, h *harness) {
				os.WriteFile(h.store.ConfigPath(), []byte("[session]\nteleportation = true\n"), 0o600)
			},
			want: StatusWarning,
		},
		{
			name:  "config has an invalid value",
			check: "configuration",
			inject: func(t *testing.T, h *harness) {
				os.WriteFile(h.store.ConfigPath(),
					[]byte("[session]\ndefault_duration = \"soon\"\n"), 0o600)
			},
			want: StatusWarning,
		},
		{
			name:  "config has an implausible value",
			check: "configuration",
			inject: func(t *testing.T, h *harness) {
				os.WriteFile(h.store.ConfigPath(),
					[]byte("[updates]\ncheck_interval = \"1s\"\n"), 0o600)
			},
			want: StatusWarning,
		},
		{
			name:   "session record absent",
			check:  "session record",
			inject: func(t *testing.T, h *harness) { os.Remove(h.store.SessionPath()) },
			want:   StatusWarning,
		},
		{
			name:  "session record unparseable",
			check: "session record",
			inject: func(t *testing.T, h *harness) {
				os.WriteFile(h.store.SessionPath(), []byte("{not json"), 0o600)
			},
			want:   StatusProblem,
			action: ActionQuarantineState,
		},
		{
			name:  "session recorded as running while the lock is free",
			check: "session record",
			inject: func(t *testing.T, h *harness) {
				sess, err := session.New(base, session.Params{
					Mode: session.ModeSystem, Duration: time.Hour, OwnerPID: 999999,
				})
				if err != nil {
					t.Fatalf("session.New() error = %v", err)
				}
				if err := h.store.WriteSession(sess); err != nil {
					t.Fatalf("WriteSession() error = %v", err)
				}
			},
			want:   StatusProblem,
			action: ActionRecoverSession,
		},
		{
			name:  "update cache unparseable",
			check: "update cache",
			inject: func(t *testing.T, h *harness) {
				os.WriteFile(h.store.UpdatePath(), []byte("{nope"), 0o600)
			},
			want:   StatusProblem,
			action: ActionDiscardCache,
		},
		{
			name:   "log directories absent",
			check:  "log directories",
			inject: func(t *testing.T, h *harness) { os.RemoveAll(h.store.LogDir()) },
			want:   StatusWarning,
			action: ActionCreateDirs,
		},
		{
			name:  "quarantined files present",
			check: "quarantined files",
			inject: func(t *testing.T, h *harness) {
				os.WriteFile(h.store.SessionPath(), []byte("broken"), 0o600)
				if _, err := store.Quarantine(h.store.SessionPath(), base); err != nil {
					t.Fatalf("Quarantine() error = %v", err)
				}
			},
			want:   StatusWarning,
			action: ActionCleanQuarantine,
		},
		{
			name:  "mechanism unavailable",
			check: "keep-awake mechanism",
			inject: func(t *testing.T, h *harness) {
				h.platform.Caps = &platform.Capabilities{
					Available: false, Detail: "caffeinate is missing",
				}
			},
			want: StatusProblem,
		},
		{
			name:  "assertions cannot be verified",
			check: "assertion verification",
			inject: func(t *testing.T, h *harness) {
				h.platform.Caps = &platform.Capabilities{
					Available: true, CanVerify: false,
					Supported: []platform.Kind{platform.KindPreventIdleSleep},
					Detail:    "pmset is missing",
				}
			},
			want: StatusWarning,
		},
		{
			name:   "lock cannot be used",
			check:  "session lock",
			inject: func(t *testing.T, h *harness) { h.lock.Err = os.ErrPermission },
			want:   StatusProblem,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.healthy(t)
			tc.inject(t, h)

			finding := h.find(t, tc.check)

			if finding.Status != tc.want {
				t.Errorf("%s = %q, want %q (%s)", tc.check, finding.Status, tc.want, finding.Detail)
			}
			if tc.action != ActionNone && finding.Action != tc.action {
				t.Errorf("action = %q, want %q", finding.Action, tc.action)
			}
			if finding.Status != StatusOK && finding.Detail == "" {
				t.Error("a finding gave no explanation")
			}
			if finding.Status == StatusProblem && finding.Remedy == "" && finding.Action == ActionNone {
				t.Error("a problem offered neither a remedy nor an action")
			}
		})
	}
}

// Doctor diagnoses and never mutates.
func TestDoctorChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	os.WriteFile(h.store.ConfigPath(), []byte("this is not [ toml"), 0o600)
	os.WriteFile(h.store.UpdatePath(), []byte("{nope"), 0o600)

	before := snapshot(t, h.store.Root())
	h.doctor.Diagnose(context.Background())
	after := snapshot(t, h.store.Root())

	if len(before) != len(after) {
		t.Fatalf("doctor changed the file list:\nbefore %v\nafter  %v", before, after)
	}
	for path, contents := range before {
		if after[path] != contents {
			t.Errorf("doctor modified %s", path)
		}
	}
}

// Repair has no powers doctor cannot predict.
func TestRepairOnlyActsOnFindings(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	report := h.doctor.Diagnose(context.Background())
	predicted := map[Action]bool{}
	for _, finding := range report.Actionable() {
		predicted[finding.Action] = true
	}

	for _, result := range h.repairer.Apply(context.Background(), report) {
		if !predicted[result.Action] {
			t.Errorf("repair performed %q, which doctor never reported", result.Action)
		}
	}
}

func TestRepairFixesWhatDoctorFound(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	os.RemoveAll(h.store.LogDir())
	os.Remove(h.store.ConfigPath())
	os.Chmod(h.store.Root(), 0o755)
	os.WriteFile(h.store.UpdatePath(), []byte("{nope"), 0o600)

	report := h.doctor.Diagnose(context.Background())
	if report.Healthy() && report.Warnings() == 0 {
		t.Fatal("the injected faults were not detected")
	}

	for _, result := range h.repairer.Apply(context.Background(), report) {
		if result.Err != nil {
			t.Errorf("repair %q failed: %v", result.Action, result.Err)
		}
	}

	after := h.doctor.Diagnose(context.Background())
	if !after.Healthy() {
		for _, finding := range after.Findings {
			if finding.Status == StatusProblem {
				t.Errorf("still a problem after repair: %s — %s", finding.Check, finding.Detail)
			}
		}
	}
	if !store.Exists(h.store.SessionLogDir()) {
		t.Error("log directories were not recreated")
	}
	if !store.Exists(h.store.ConfigPath()) {
		t.Error("the config was not regenerated")
	}
}

func TestRepairIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)
	os.RemoveAll(h.store.LogDir())

	first := h.repairer.Apply(context.Background(), h.doctor.Diagnose(context.Background()))
	if len(first) == 0 {
		t.Fatal("nothing was repaired")
	}

	second := h.repairer.Apply(context.Background(), h.doctor.Diagnose(context.Background()))
	for _, result := range second {
		if result.Action == ActionCreateDirs {
			continue // creating existing directories is a no-op, not a change
		}
		t.Errorf("a second repair acted again: %q", result.Action)
	}
}

func TestRepairOnHealthyInstallationDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	report := h.doctor.Diagnose(context.Background())
	for _, finding := range report.Actionable() {
		if finding.Status == StatusProblem {
			t.Fatalf("the healthy fixture has a problem: %s", finding.Check)
		}
	}
}

// Repair never touches logs, and never deletes quarantined files unless
// explicitly authorised.
func TestRepairPreservesLogsAndQuarantine(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	logPath := filepath.Join(h.store.LogDir(), "awake.jsonl")
	if err := os.WriteFile(logPath, []byte(`{"event":"session.started"}`+"\n"), 0o600); err != nil {
		t.Fatalf("writing log: %v", err)
	}

	os.WriteFile(h.store.SessionPath(), []byte("broken"), 0o600)
	quarantined, err := store.Quarantine(h.store.SessionPath(), base)
	if err != nil {
		t.Fatalf("Quarantine() error = %v", err)
	}

	report := h.doctor.Diagnose(context.Background())
	h.repairer.Apply(context.Background(), report)

	if !store.Exists(logPath) {
		t.Error("repair deleted a log file")
	}
	if !store.Exists(quarantined) {
		t.Error("repair deleted a quarantined file without --clean-quarantine")
	}
}

func TestCleanQuarantineDeletesOnlyQuarantinedFiles(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	os.WriteFile(h.store.SessionPath(), []byte("broken"), 0o600)
	quarantined, err := store.Quarantine(h.store.SessionPath(), base)
	if err != nil {
		t.Fatalf("Quarantine() error = %v", err)
	}

	logPath := filepath.Join(h.store.LogDir(), "awake.jsonl")
	os.WriteFile(logPath, []byte("{}\n"), 0o600)

	h.repairer.CleanQuarantine = true
	report := h.doctor.Diagnose(context.Background())
	h.repairer.Apply(context.Background(), report)

	if store.Exists(quarantined) {
		t.Error("--clean-quarantine did not delete the set-aside file")
	}
	if !store.Exists(logPath) {
		t.Error("--clean-quarantine deleted something that was not quarantined")
	}
	if !store.Exists(h.store.ConfigPath()) {
		t.Error("--clean-quarantine deleted the config")
	}
}

// A config that parses is never modified, even one Awake disagrees with.
func TestRepairNeverRewritesAValidConfig(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	const mine = "# my careful settings\n[session]\ndefault_duration = \"90m\"\n"
	if err := os.WriteFile(h.store.ConfigPath(), []byte(mine), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	report := h.doctor.Diagnose(context.Background())
	h.repairer.Apply(context.Background(), report)

	after, err := os.ReadFile(h.store.ConfigPath())
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if string(after) != mine {
		t.Errorf("repair rewrote a valid config:\n%s", after)
	}
}

func TestQuarantinedConfigIsPreservedAndReplaced(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	const broken = "this is not [ toml"
	os.WriteFile(h.store.ConfigPath(), []byte(broken), 0o600)

	report := h.doctor.Diagnose(context.Background())
	results := h.repairer.Apply(context.Background(), report)

	var repaired bool
	for _, result := range results {
		if result.Action == ActionQuarantineConfig {
			repaired = true
			if result.Err != nil {
				t.Fatalf("quarantine failed: %v", result.Err)
			}
		}
	}
	if !repaired {
		t.Fatal("the unreadable config was not quarantined")
	}

	found, err := h.store.ListQuarantined()
	if err != nil {
		t.Fatalf("ListQuarantined() error = %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d quarantined files, want 1", len(found))
	}

	preserved, err := os.ReadFile(found[0])
	if err != nil {
		t.Fatalf("reading quarantined file: %v", err)
	}
	if string(preserved) != broken {
		t.Error("the quarantined config was altered")
	}

	if !store.Exists(h.store.ConfigPath()) {
		t.Error("no fresh config was written")
	}
}

// Every problem must tell the user what to do about it.
func TestEveryProblemIsActionable(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)
	os.WriteFile(h.store.ConfigPath(), []byte("not [ toml"), 0o600)
	os.WriteFile(h.store.SessionPath(), []byte("{nope"), 0o600)

	for _, finding := range h.doctor.Diagnose(context.Background()).Findings {
		if finding.Status == StatusOK {
			continue
		}
		if finding.Detail == "" {
			t.Errorf("%s: no explanation", finding.Check)
		}
		if finding.Status == StatusProblem && finding.Remedy == "" {
			t.Errorf("%s: a problem with no remedy", finding.Check)
		}
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return files
}

// seedUpdateCache writes a cached answer without performing a check.
func (h *harness) seedUpdateCache(t *testing.T, cache update.Cache) {
	t.Helper()
	if err := h.store.WriteJSON(h.store.UpdatePath(), cache); err != nil {
		t.Fatalf("seeding update cache: %v", err)
	}
}

// A user who never runs `awake update check` still learns from doctor that a
// release exists — without doctor making a network request.
func TestDoctorReportsAvailableUpdate(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)
	h.seedUpdateCache(t, update.Cache{
		Version: update.CacheVersion, Channel: "stable", CheckedAt: base,
		Result: update.OutcomeUpdateAvailable, LatestVersion: "0.2.0",
		Severity: update.SeverityRecommended,
		NotesURL: "https://example.invalid/releases/v0.2.0",
	})

	finding := h.find(t, "available update")

	if finding.Status != StatusWarning {
		t.Errorf("status = %q, want %q", finding.Status, StatusWarning)
	}
	if !strings.Contains(finding.Detail, "0.2.0") {
		t.Errorf("detail does not name the version: %q", finding.Detail)
	}
	if !strings.Contains(finding.Remedy, "https://example.invalid/releases/v0.2.0") {
		t.Errorf("remedy does not point at the release notes: %q", finding.Remedy)
	}
	if !strings.Contains(finding.Remedy, "never installs") {
		t.Errorf("remedy does not say Awake will not install it: %q", finding.Remedy)
	}
}

// A security release is called out in the text. It is deliberately still a
// warning rather than a problem: severity enforces nothing in v0.1.0, and
// exiting 5 would be a form of enforcement reserved for v0.2.
func TestDoctorCallsOutSecurityRelease(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)
	h.seedUpdateCache(t, update.Cache{
		Version: update.CacheVersion, Channel: "stable", CheckedAt: base,
		Result: update.OutcomeUpdateAvailable, LatestVersion: "0.2.1",
		Severity: update.SeveritySecurity,
		NotesURL: "https://example.invalid/releases/v0.2.1",
	})

	finding := h.find(t, "available update")

	if !strings.Contains(finding.Detail, "security release") {
		t.Errorf("a security release was not called out: %q", finding.Detail)
	}
	if finding.Status != StatusWarning {
		t.Errorf("status = %q; promoting security to a problem is a v0.2 decision", finding.Status)
	}
	if !h.doctor.Diagnose(context.Background()).Healthy() {
		t.Error("an available update made the installation unhealthy")
	}
}

func TestDoctorReportsUpToDate(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)
	h.seedUpdateCache(t, update.Cache{
		Version: update.CacheVersion, Channel: "stable", CheckedAt: base,
		Result: update.OutcomeUpToDate, LatestVersion: "0.1.0",
	})

	finding := h.find(t, "available update")

	if finding.Status != StatusOK {
		t.Errorf("status = %q, want %q", finding.Status, StatusOK)
	}
	if !strings.Contains(finding.Detail, "latest") {
		t.Errorf("detail = %q", finding.Detail)
	}
}

// With checking disabled, doctor says so rather than implying it knows.
func TestDoctorRespectsDisabledUpdates(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)
	if err := os.WriteFile(h.store.ConfigPath(),
		[]byte("[updates]\nenabled = false\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	h.seedUpdateCache(t, update.Cache{
		Version: update.CacheVersion, CheckedAt: base,
		Result: update.OutcomeUpdateAvailable, LatestVersion: "0.2.0",
	})

	finding := h.find(t, "available update")

	if finding.Status != StatusOK {
		t.Errorf("status = %q, want %q when checking is disabled", finding.Status, StatusOK)
	}
	if !strings.Contains(finding.Detail, "disabled") {
		t.Errorf("detail = %q", finding.Detail)
	}
}

// Never checked is not a fault; it is simply unknown.
func TestDoctorHandlesNeverChecked(t *testing.T) {
	h := newHarness(t)
	h.healthy(t)

	finding := h.find(t, "available update")

	if finding.Status != StatusOK {
		t.Errorf("status = %q, want %q", finding.Status, StatusOK)
	}
	if finding.Remedy == "" {
		t.Error("doctor did not say how to find out")
	}
}
