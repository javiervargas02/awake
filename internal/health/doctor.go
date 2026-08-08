package health

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/javiervargas02/awake/internal/clock"
	"github.com/javiervargas02/awake/internal/config"
	"github.com/javiervargas02/awake/internal/lock"
	"github.com/javiervargas02/awake/internal/platform"
	"github.com/javiervargas02/awake/internal/store"
	"github.com/javiervargas02/awake/internal/update"
)

// Doctor inspects an installation. It never mutates anything.
type Doctor struct {
	store    *store.Store
	platform platform.Controller
	lock     lock.Guard
	clock    clock.Clock
}

func NewDoctor(st *store.Store, controller platform.Controller, guard lock.Guard, c clock.Clock) *Doctor {
	if c == nil {
		c = clock.System{}
	}
	return &Doctor{store: st, platform: controller, lock: guard, clock: c}
}

// Diagnose runs every check and returns what it found.
//
// The checks are ordered so that a reader works outwards from the directory to
// its contents to the platform: a missing root explains everything after it.
func (d *Doctor) Diagnose(ctx context.Context) Report {
	var report Report

	add := func(f Finding) { report.Findings = append(report.Findings, f) }

	// Loaded once and shared: two checks need it, and reading the file twice
	// could report two different states if it changed in between.
	cfg, cfgReport, cfgErr := config.Load(d.store.ConfigPath())

	add(d.checkRoot())
	add(d.checkPermissions())
	add(d.checkConfig(cfgReport, cfgErr))
	add(d.checkSessionRecord(ctx))
	add(d.checkLock())
	add(d.checkLogDirs())
	add(d.checkWritable())
	add(d.checkUpdateCache())
	add(d.checkAvailableUpdate(cfg))
	add(d.checkPlatform())
	add(d.checkVerification())
	add(d.checkQuarantine())

	return report
}

func (d *Doctor) checkRoot() Finding {
	f := Finding{Check: "state directory"}

	info, err := os.Stat(d.store.Root())
	switch {
	case errors.Is(err, os.ErrNotExist):
		// Normal before the first session, and after the user deletes it.
		f.Status = StatusWarning
		f.Detail = fmt.Sprintf("%s does not exist yet", d.store.Root())
		f.Remedy = "nothing to do; it is created when a session starts"
		f.Action, f.Target = ActionCreateDirs, d.store.Root()
		return f

	case err != nil:
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("%s cannot be inspected: %v", d.store.Root(), err)
		return f

	case !info.IsDir():
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("%s exists but is not a directory", d.store.Root())
		f.Remedy = "move or delete that file, then run any awake command"
		return f
	}

	f.Status = StatusOK
	f.Detail = fmt.Sprintf("%s exists", d.store.Root())
	return f
}

func (d *Doctor) checkPermissions() Finding {
	f := Finding{Check: "state directory permissions"}

	info, err := os.Stat(d.store.Root())
	if err != nil {
		f.Status = StatusOK
		f.Detail = "not applicable; the directory does not exist"
		return f
	}

	mode := info.Mode().Perm()
	switch {
	case mode&0o077 != 0:
		// Even with no content logged about the user, the timing of sessions
		// reveals when someone was away from their machine.
		f.Status = StatusWarning
		f.Detail = fmt.Sprintf("%s is readable by other users (mode %04o)", d.store.Root(), mode)
		f.Remedy = "run 'awake repair' to restrict it to you"
		f.Action, f.Target = ActionFixPermissions, d.store.Root()

	case mode&0o700 != 0o700:
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("%s is not fully accessible to you (mode %04o)", d.store.Root(), mode)
		f.Remedy = "run 'awake repair'"
		f.Action, f.Target = ActionFixPermissions, d.store.Root()

	default:
		f.Status = StatusOK
		f.Detail = fmt.Sprintf("mode %04o", mode)
	}
	return f
}

func (d *Doctor) checkConfig(report config.Report, err error) Finding {
	f := Finding{Check: "configuration"}
	path := d.store.ConfigPath()

	if !store.Exists(path) {
		f.Status = StatusWarning
		f.Detail = "no config file; built-in defaults are in use"
		f.Remedy = "run 'awake repair' to write a documented default config"
		f.Action, f.Target = ActionGenerateConfig, path
		return f
	}

	if err != nil {
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("%s cannot be parsed: %v", path, err)
		f.Remedy = "run 'awake repair' to set it aside and write a fresh one"
		f.Action, f.Target = ActionQuarantineConfig, path
		return f
	}

	// A config that parses is never touched, even one we disagree with.
	switch {
	case len(report.Defaulted) > 0:
		f.Status = StatusWarning
		f.Detail = fmt.Sprintf("%d setting(s) fell back to defaults: %s",
			len(report.Defaulted), joinDefaulted(report))
		f.Remedy = "correct the value, or delete the line to use the default"

	case len(report.Implausible) > 0:
		f.Status = StatusWarning
		f.Detail = report.Implausible[0].Key + " " + report.Implausible[0].Detail
		f.Remedy = "the value is honoured; change it if it was not intended"

	case len(report.UnknownKeys) > 0:
		f.Status = StatusWarning
		f.Detail = fmt.Sprintf("unrecognised setting(s): %v", report.UnknownKeys)
		f.Remedy = "check for a typo, or a setting from a newer version"

	default:
		f.Status = StatusOK
		f.Detail = "loaded"
	}
	return f
}

func (d *Doctor) checkSessionRecord(ctx context.Context) Finding {
	f := Finding{Check: "session record"}
	path := d.store.SessionPath()

	record, err := d.store.ReadSession()
	switch {
	case errors.Is(err, store.ErrNotFound):
		f.Status = StatusWarning
		f.Detail = "no sessions have run on this computer yet"
		return f

	case errors.Is(err, store.ErrCorrupt):
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("%s cannot be understood: %v", path, err)
		f.Remedy = "run 'awake repair' to set it aside"
		f.Action, f.Target = ActionQuarantineState, path
		return f

	case err != nil:
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("%s cannot be read: %v", path, err)
		return f

	case record.Status.IsTerminal():
		f.Status = StatusOK
		f.Detail = fmt.Sprintf("last session %s (%s)", record.Status, record.EndReason)
		return f
	}

	// A non-terminal record is only meaningful alongside the lock: it is the
	// lock, not the record, that says whether a session is genuinely live.
	if d.sessionIsLive() {
		f.Status = StatusOK
		f.Detail = "a session is running"
		return f
	}

	f.Status = StatusProblem
	f.Detail = "a session is recorded as running, but nothing holds the lock; its process died"
	f.Remedy = "run 'awake repair', or simply start a session — recovery is automatic"
	f.Action, f.Target = ActionRecoverSession, path
	return f
}

// sessionIsLive reports whether something holds the exclusivity lock.
func (d *Doctor) sessionIsLive() bool {
	acquired, err := d.lock.TryAcquire()
	if err != nil {
		// Cannot consult the lock. Assume a live session rather than declaring
		// one dead on a guess.
		return true
	}
	if acquired {
		d.lock.Release()
		return false
	}
	return true
}

func (d *Doctor) checkLock() Finding {
	f := Finding{Check: "session lock"}

	acquired, err := d.lock.TryAcquire()
	switch {
	case err != nil:
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("the lock at %s cannot be used: %v", d.lock.Path(), err)
		f.Remedy = "sessions still run, but two could start at once; check that the directory is writable"
		return f

	case acquired:
		d.lock.Release()
		f.Status = StatusOK
		f.Detail = "available"

	default:
		f.Status = StatusOK
		f.Detail = "held by a running session"
	}
	return f
}

func (d *Doctor) checkLogDirs() Finding {
	f := Finding{Check: "log directories"}

	for _, dir := range []string{d.store.LogDir(), d.store.SessionLogDir()} {
		info, err := os.Stat(dir)
		switch {
		case errors.Is(err, os.ErrNotExist):
			f.Status = StatusWarning
			f.Detail = fmt.Sprintf("%s does not exist yet", dir)
			f.Remedy = "run 'awake repair', or start a session"
			f.Action, f.Target = ActionCreateDirs, dir
			return f

		case err != nil:
			f.Status = StatusProblem
			f.Detail = fmt.Sprintf("%s cannot be inspected: %v", dir, err)
			return f

		case info.Mode().Perm()&0o300 != 0o300:
			f.Status = StatusProblem
			f.Detail = fmt.Sprintf("%s is not writable", dir)
			f.Remedy = "run 'awake repair'"
			f.Action, f.Target = ActionFixPermissions, dir
			return f
		}
	}

	f.Status = StatusOK
	f.Detail = "present and writable"
	return f
}

// checkWritable proves the state directory works, rather than inferring it
// from permissions: the write path is what actually matters.
func (d *Doctor) checkWritable() Finding {
	f := Finding{Check: "write probe"}

	if !store.Exists(d.store.Root()) {
		f.Status = StatusOK
		f.Detail = "not applicable; the directory does not exist"
		return f
	}

	probe, err := os.CreateTemp(d.store.Root(), ".awake-probe-*")
	if err != nil {
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("cannot write to %s: %v", d.store.Root(), err)
		f.Remedy = "check the directory's permissions and available disk space"
		return f
	}
	name := probe.Name()
	probe.Close()

	if err := os.Rename(name, name+".renamed"); err != nil {
		os.Remove(name)
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("cannot replace files in %s: %v", d.store.Root(), err)
		f.Remedy = "state is written by rename; this filesystem may not support it"
		return f
	}
	os.Remove(name + ".renamed")

	f.Status = StatusOK
	f.Detail = "writable"
	return f
}

func (d *Doctor) checkUpdateCache() Finding {
	f := Finding{Check: "update cache"}
	path := d.store.UpdatePath()

	if !store.Exists(path) {
		f.Status = StatusWarning
		f.Detail = "no cached update check yet"
		return f
	}

	var cached update.Cache
	if err := d.store.ReadJSON(path, &cached); errors.Is(err, store.ErrCorrupt) {
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("%s cannot be understood", path)
		f.Remedy = "run 'awake repair' to discard it; it is only a cache"
		f.Action, f.Target = ActionDiscardCache, path
		return f
	}

	// Reporting the age is the only mitigation available for the one attack
	// ADR-0009 leaves open: a suppressed security notification. A stale answer
	// should look stale rather than implying freshness.
	age := cached.Age(d.clock.Now())
	switch {
	case cached.CheckedAt.IsZero():
		f.Status = StatusWarning
		f.Detail = "no cached update check yet"

	case age > 30*24*time.Hour:
		f.Status = StatusWarning
		f.Detail = fmt.Sprintf("last checked %d days ago; you may be unaware of a newer release",
			int(age.Hours()/24))
		f.Remedy = "run 'awake update check --force'"

	default:
		f.Status = StatusOK
		f.Detail = fmt.Sprintf("last checked %s ago (%s)", age.Round(time.Minute), cached.Result)
	}
	return f
}

// checkAvailableUpdate reports what the last check found, without performing
// one.
//
// This reads the cache and never touches the network, which is what keeps it
// inside v0.1.0's rule that `awake update check` is the only thing that reaches
// out. Without it, a user who never runs that command would never learn a
// release exists — and doctor is where someone looks when they want to know
// whether anything needs attention.
func (d *Doctor) checkAvailableUpdate(cfg config.Config) Finding {
	f := Finding{Check: "available update"}

	if !cfg.Updates.Enabled {
		f.Status = StatusOK
		f.Detail = "update checking is disabled; Awake makes no network requests"
		return f
	}

	var cached update.Cache
	if err := d.store.ReadJSON(d.store.UpdatePath(), &cached); err != nil || cached.CheckedAt.IsZero() {
		f.Status = StatusOK
		f.Detail = "not known yet"
		f.Remedy = "run 'awake update check' to find out"
		return f
	}

	switch cached.Result {
	case update.OutcomeUpdateAvailable:
		f.Status = StatusWarning
		f.Detail = fmt.Sprintf("Awake %s is available", cached.LatestVersion)
		if cached.Severity == update.SeveritySecurity {
			f.Detail += " — this is a security release"
		}

		// Say where to read about it, and that Awake will not install it.
		if cached.NotesURL != "" {
			f.Remedy = "release notes: " + cached.NotesURL + " (Awake never installs updates)"
		} else {
			f.Remedy = "run 'awake update check' for details (Awake never installs updates)"
		}

	case update.OutcomeUpToDate:
		f.Status = StatusOK
		f.Detail = "you are on the latest release"

	default:
		// A failed or inconclusive check is not a fault in the installation.
		f.Status = StatusOK
		f.Detail = "the last check did not produce an answer"
		f.Remedy = "run 'awake update check --force' to try again"
	}

	return f
}

func (d *Doctor) checkPlatform() Finding {
	f := Finding{Check: "keep-awake mechanism"}

	caps := d.platform.Describe()
	if !caps.Available {
		f.Status = StatusProblem
		f.Detail = caps.Detail
		f.Remedy = "sessions cannot run on this machine until this is resolved"
		return f
	}

	f.Status = StatusOK
	f.Detail = fmt.Sprintf("%s at %s", caps.Mechanism, caps.Path)
	return f
}

// checkVerification reports whether Awake can confirm its own effect. A
// machine that cannot verify should say so before a session is started rather
// than midway through one.
func (d *Doctor) checkVerification() Finding {
	f := Finding{Check: "assertion verification"}

	caps := d.platform.Describe()
	switch {
	case !caps.Available:
		f.Status = StatusOK
		f.Detail = "not applicable; no mechanism is available"

	case !caps.CanVerify:
		f.Status = StatusWarning
		f.Detail = "sessions will run unverified: " + caps.Detail
		f.Remedy = "sessions still work; Awake just cannot confirm the system agreed"

	default:
		f.Status = StatusOK
		f.Detail = "available"
	}
	return f
}

func (d *Doctor) checkQuarantine() Finding {
	f := Finding{Check: "quarantined files"}

	if !store.Exists(d.store.Root()) {
		f.Status = StatusOK
		f.Detail = "none"
		return f
	}

	found, err := d.store.ListQuarantined()
	if err != nil {
		f.Status = StatusProblem
		f.Detail = fmt.Sprintf("cannot scan for set-aside files: %v", err)
		return f
	}
	if len(found) == 0 {
		f.Status = StatusOK
		f.Detail = "none"
		return f
	}

	// Never cleaned up automatically: deleting a user's file is their call.
	f.Status = StatusWarning
	f.Detail = fmt.Sprintf("%d file(s) set aside after being found corrupt, kept for inspection", len(found))
	f.Remedy = "inspect them, then run 'awake repair --clean-quarantine' to delete them"
	f.Action, f.Target = ActionCleanQuarantine, filepath.Dir(found[0])
	return f
}

func joinDefaulted(report config.Report) string {
	var keys string
	for i, defaulted := range report.Defaulted {
		if i > 0 {
			keys += ", "
		}
		keys += defaulted.Key
	}
	return keys
}
