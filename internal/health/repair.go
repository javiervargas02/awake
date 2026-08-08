package health

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/javiervargas02/awake/internal/clock"
	"github.com/javiervargas02/awake/internal/config"
	"github.com/javiervargas02/awake/internal/store"
)

// Outcomes name files by their base name, never their full path. An absolute
// path under a home directory carries the user's name, and principle 5 keeps
// anything that is not Awake's own business out of the logs.

// Result is one repair that was attempted.
type Result struct {
	Action Action `json:"action"`
	Target string `json:"target"`

	// Outcome is what happened, in plain language.
	Outcome string `json:"outcome"`

	// Err is set when the repair failed. The others are still attempted: one
	// broken fix should not block the rest.
	Err error `json:"-"`
}

// Repairer applies safe fixes.
//
// Every action maps to a finding doctor would have reported, and nothing here
// acts on anything doctor did not report. That is what keeps the pair honest:
// repair has no powers doctor cannot predict.
type Repairer struct {
	store *store.Store
	clock clock.Clock

	// CleanQuarantine authorises deleting set-aside files. It is off by
	// default because deleting a user's files is their decision, and it is
	// narrow enough that the whole blast radius fits in one sentence: files
	// matching the quarantine naming pattern, and nothing else.
	CleanQuarantine bool
}

func NewRepairer(st *store.Store, c clock.Clock) *Repairer {
	if c == nil {
		c = clock.System{}
	}
	return &Repairer{store: st, clock: c}
}

// Apply performs the repairs a report calls for.
//
// It is idempotent: running it twice changes nothing the second time. On a
// healthy installation it does nothing and returns no results.
func (r *Repairer) Apply(ctx context.Context, report Report) []Result {
	var results []Result

	for _, finding := range report.Actionable() {
		if finding.Action == ActionCleanQuarantine && !r.CleanQuarantine {
			continue
		}
		results = append(results, r.perform(ctx, finding))
	}

	return results
}

func (r *Repairer) perform(_ context.Context, finding Finding) Result {
	result := Result{Action: finding.Action, Target: finding.Target}

	switch finding.Action {
	case ActionCreateDirs:
		if err := r.store.EnsureDirs(); err != nil {
			result.Err = err
			break
		}
		result.Outcome = "created the state and log directories"

	case ActionFixPermissions:
		// Tighten only, never loosen.
		if err := r.store.EnsureDirs(); err != nil {
			result.Err = err
			break
		}
		result.Outcome = "restricted the state directories to your user"

	case ActionGenerateConfig:
		if err := config.Generate(r.store.ConfigPath(), 0o600); err != nil {
			result.Err = err
			break
		}
		result.Outcome = "wrote a documented default config"

	case ActionQuarantineConfig:
		moved, err := store.Quarantine(finding.Target, r.clock.Now())
		if err != nil {
			result.Err = err
			break
		}
		if genErr := config.Generate(r.store.ConfigPath(), 0o600); genErr != nil {
			result.Err = genErr
			result.Outcome = fmt.Sprintf("set the unreadable config aside as %s", filepath.Base(moved))
			break
		}
		result.Outcome = fmt.Sprintf("set the unreadable config aside as %s and wrote a fresh one", filepath.Base(moved))

	case ActionQuarantineState:
		moved, err := store.Quarantine(finding.Target, r.clock.Now())
		if err != nil {
			result.Err = err
			break
		}
		result.Outcome = fmt.Sprintf("set the unreadable session record aside as %s", filepath.Base(moved))

	case ActionRecoverSession:
		// The application service owns recovery, because it involves the
		// platform and the session's own trace. Repair only reports that it
		// will happen; it does not duplicate the logic.
		result.Outcome = "the crashed session is resolved automatically by the next command"

	case ActionDiscardCache:
		if err := os.Remove(finding.Target); err != nil && !os.IsNotExist(err) {
			result.Err = err
			break
		}
		result.Outcome = "discarded the update cache; it will be rebuilt on the next check"

	case ActionCleanQuarantine:
		deleted, err := r.cleanQuarantine()
		if err != nil {
			result.Err = err
			break
		}
		result.Outcome = fmt.Sprintf("deleted %d set-aside file(s)", deleted)

	default:
		result.Err = fmt.Errorf("unknown repair action %q", finding.Action)
	}

	if result.Err != nil {
		result.Outcome = "failed: " + result.Err.Error()
	}
	return result
}

// cleanQuarantine deletes set-aside files, and only those.
//
// The paths come from the store's own scan rather than from a caller, so the
// action cannot be pointed at anything else.
func (r *Repairer) cleanQuarantine() (int, error) {
	found, err := r.store.ListQuarantined()
	if err != nil {
		return 0, err
	}

	var deleted int
	for _, path := range found {
		if !store.IsQuarantined(path) {
			// Belt and braces: never delete something that does not carry the
			// quarantine marker.
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
