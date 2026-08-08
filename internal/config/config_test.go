package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	return path
}

func TestMissingFileIsHealthy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v; a missing config is not a failure", err)
	}
	if report.Source != SourceDefaults {
		t.Errorf("source = %q, want %q", report.Source, SourceDefaults)
	}
	if len(report.Defaulted) != 0 {
		t.Errorf("missing file reported per-key fallbacks: %+v", report.Defaulted)
	}
	if cfg != Defaults() {
		t.Errorf("config = %+v, want defaults %+v", cfg, Defaults())
	}
}

func TestLoadsValues(t *testing.T) {
	path := writeConfig(t, `
[session]
default_duration = "45m"

[updates]
enabled = false
check_interval = "12h"
`)

	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if report.Source != SourceFile {
		t.Errorf("source = %q, want %q", report.Source, SourceFile)
	}
	if cfg.Session.DefaultDuration != 45*time.Minute {
		t.Errorf("default_duration = %v, want 45m", cfg.Session.DefaultDuration)
	}
	if cfg.Updates.Enabled {
		t.Error("enabled = true, want false")
	}
	if cfg.Updates.CheckInterval != 12*time.Hour {
		t.Errorf("check_interval = %v, want 12h", cfg.Updates.CheckInterval)
	}
}

// A partial file overrides only what it mentions. This is what makes deleting
// part of the config safe.
func TestPartialConfigKeepsOtherDefaults(t *testing.T) {
	path := writeConfig(t, "[session]\ndefault_duration = \"5m\"\n")

	cfg, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Session.DefaultDuration != 5*time.Minute {
		t.Errorf("default_duration = %v, want 5m", cfg.Session.DefaultDuration)
	}
	if cfg.Updates.CheckInterval != Defaults().Updates.CheckInterval {
		t.Errorf("check_interval = %v, want the default", cfg.Updates.CheckInterval)
	}
	if cfg.Updates.Enabled != Defaults().Updates.Enabled {
		t.Error("enabled did not keep its default")
	}
}

// ADR-0007: an invalid value degrades that key alone, never the whole file.
func TestInvalidValueDegradesOneKey(t *testing.T) {
	path := writeConfig(t, `
[session]
default_duration = "half an hour"

[updates]
check_interval = "12h"
`)

	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v; one bad value must not fail the file", err)
	}
	if cfg.Session.DefaultDuration != Defaults().Session.DefaultDuration {
		t.Errorf("default_duration = %v, want the default", cfg.Session.DefaultDuration)
	}
	if cfg.Updates.CheckInterval != 12*time.Hour {
		t.Errorf("check_interval = %v, want 12h; a good key must survive a bad one",
			cfg.Updates.CheckInterval)
	}

	if len(report.Defaulted) != 1 {
		t.Fatalf("defaulted = %+v, want exactly one entry", report.Defaulted)
	}
	if got := report.Defaulted[0]; got.Key != "session.default_duration" || got.Reason != ReasonInvalidValue {
		t.Errorf("defaulted[0] = %+v, want session.default_duration/invalid_value", got)
	}
}

func TestNegativeDurationIsRejected(t *testing.T) {
	path := writeConfig(t, "[session]\ndefault_duration = \"-5m\"\n")

	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Session.DefaultDuration != Defaults().Session.DefaultDuration {
		t.Error("a negative duration was accepted")
	}
	if len(report.Defaulted) != 1 {
		t.Errorf("defaulted = %+v, want one entry", report.Defaulted)
	}
}

// A value can be valid and still be wrong for its purpose: a one-second check
// interval parses fine and would defeat the update cache entirely.
func TestImplausibleValuesWarnButAreHonoured(t *testing.T) {
	path := writeConfig(t, "[updates]\ncheck_interval = \"1s\"\n")

	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Updates.CheckInterval != time.Second {
		t.Errorf("check_interval = %v, want 1s; it is the user's machine", cfg.Updates.CheckInterval)
	}
	if len(report.Implausible) != 1 {
		t.Fatalf("implausible = %+v, want one entry", report.Implausible)
	}
	if report.Implausible[0].Key != "updates.check_interval" {
		t.Errorf("implausible key = %q", report.Implausible[0].Key)
	}
	if len(report.Defaulted) != 0 {
		t.Errorf("an implausible value was treated as invalid: %+v", report.Defaulted)
	}
}

func TestPlausibleValuesDoNotWarn(t *testing.T) {
	path := writeConfig(t, "[updates]\ncheck_interval = \"24h\"\n")

	_, report, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(report.Implausible) != 0 {
		t.Errorf("a normal value warned: %+v", report.Implausible)
	}
}

func TestUnknownKeysWarnAndStillWork(t *testing.T) {
	path := writeConfig(t, `
[session]
default_duration = "45m"
teleportation = true

[future]
setting = "from a newer version"
`)

	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v; unknown keys must not fail the file", err)
	}
	if cfg.Session.DefaultDuration != 45*time.Minute {
		t.Errorf("default_duration = %v, want 45m", cfg.Session.DefaultDuration)
	}
	if len(report.UnknownKeys) != 2 {
		t.Errorf("unknown keys = %v, want 2", report.UnknownKeys)
	}
}

func TestUnparseableFileFallsBackAndReports(t *testing.T) {
	path := writeConfig(t, "this is not [ valid toml at all")

	cfg, report, err := Load(path)
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Load() error = %v, want ErrUnreadable", err)
	}
	if cfg != Defaults() {
		t.Error("an unparseable file did not fall back to defaults")
	}
	if report.Source != SourceDefaults {
		t.Errorf("source = %q, want %q", report.Source, SourceDefaults)
	}
	if len(report.Defaulted) != 1 || report.Defaulted[0].Reason != ReasonUnreadable {
		t.Errorf("defaulted = %+v, want one unreadable entry", report.Defaulted)
	}
}

// Loading never repairs: the caller decides whether to quarantine.
func TestLoadNeverModifiesTheFile(t *testing.T) {
	const contents = "this is not [ valid toml at all"
	path := writeConfig(t, contents)

	if _, _, err := Load(path); err == nil {
		t.Fatal("expected an error for an unparseable file")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file was removed by a read: %v", err)
	}
	if string(after) != contents {
		t.Error("Load() modified the config file")
	}
}

func TestGeneratedFileLoadsAsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	if err := Generate(path, 0o600); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	cfg, report, err := Load(path)
	if err != nil {
		t.Fatalf("Load() on generated file error = %v", err)
	}
	if cfg != Defaults() {
		t.Errorf("generated config loaded as %+v, want defaults %+v", cfg, Defaults())
	}
	if len(report.UnknownKeys) != 0 {
		t.Errorf("generated config has keys the loader does not know: %v", report.UnknownKeys)
	}
	if len(report.Defaulted) != 0 || len(report.Implausible) != 0 {
		t.Errorf("generated config produced warnings: %+v %+v", report.Defaulted, report.Implausible)
	}
}

// The generated file is documentation as much as configuration: every key the
// loader understands must be explained in it.
func TestGeneratedFileDocumentsEveryKey(t *testing.T) {
	rendered := Render()

	for _, key := range Keys() {
		leaf := key[strings.LastIndex(key, ".")+1:]
		if !containsKey(rendered, leaf) {
			t.Errorf("generated config does not mention %q", key)
		}
	}
	if !strings.Contains(rendered, "safe") {
		t.Error("generated config does not tell the user that deleting it is safe")
	}
}

func TestGenerateRefusesToOverwrite(t *testing.T) {
	const mine = "# my careful settings\n[session]\ndefault_duration = \"90m\"\n"
	path := writeConfig(t, mine)

	if err := Generate(path, 0o600); err == nil {
		t.Fatal("Generate() overwrote an existing config")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if string(after) != mine {
		t.Error("Generate() modified a config the user owns")
	}
}

func TestGeneratedFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Generate(path, 0o600); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %04o, want 0600", got)
	}
}
