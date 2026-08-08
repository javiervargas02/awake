// Package health diagnoses and repairs Awake's own installation.
//
// It implements the state-and-repair architecture. Two rules shape it:
//
// Doctor diagnoses and never mutates. Repair performs only safe fixes, each
// one mapping to a finding doctor would have reported — so repair has no
// powers doctor cannot predict, and a user can always see the whole blast
// radius before authorising it.
package health

// Status is the severity of a finding.
//
// The distinction between warning and problem is what lets doctor's exit code
// mean something: a machine with no config file and no history is healthy, not
// broken.
type Status string

const (
	// StatusOK: nothing to say.
	StatusOK Status = "ok"
	// StatusWarning: informational. Something is absent or unusual, and Awake
	// works correctly regardless.
	StatusWarning Status = "warning"
	// StatusProblem: something needs attention.
	StatusProblem Status = "problem"
)

func (s Status) String() string { return string(s) }

// Action identifies a repair. Every problem doctor reports names the action
// that would fix it, or names none — and repair may only perform actions
// doctor asked for.
type Action string

const (
	ActionNone Action = ""

	ActionCreateDirs       Action = "create_directories"
	ActionFixPermissions   Action = "fix_permissions"
	ActionGenerateConfig   Action = "generate_config"
	ActionQuarantineConfig Action = "quarantine_config"
	ActionQuarantineState  Action = "quarantine_session_record"
	ActionRecoverSession   Action = "recover_session"
	ActionDiscardCache     Action = "discard_update_cache"
	ActionCleanQuarantine  Action = "clean_quarantine"
)

// Finding is one check's result.
type Finding struct {
	// Check names what was inspected.
	Check string `json:"check"`

	Status Status `json:"status"`

	// Detail explains the finding in plain language.
	Detail string `json:"detail"`

	// Remedy tells the user what to do. For anything repair can fix, it names
	// that command — which is what makes doctor the dry run for repair.
	Remedy string `json:"remedy,omitempty"`

	// Action is the repair that would resolve this, if any.
	Action Action `json:"-"`

	// Target is the path the action applies to.
	Target string `json:"-"`
}

// Report is the whole diagnosis.
type Report struct {
	Findings []Finding `json:"findings"`
}

func (r Report) count(status Status) int {
	var n int
	for _, finding := range r.Findings {
		if finding.Status == status {
			n++
		}
	}
	return n
}

func (r Report) OK() int       { return r.count(StatusOK) }
func (r Report) Warnings() int { return r.count(StatusWarning) }
func (r Report) Problems() int { return r.count(StatusProblem) }

// Healthy reports whether anything needs attention. Warnings do not count:
// they are informational, and a script should be able to tell the difference
// without parsing output.
func (r Report) Healthy() bool { return r.Problems() == 0 }

// Actionable returns the findings repair could act on, in the order they were
// reported.
func (r Report) Actionable() []Finding {
	var actionable []Finding
	for _, finding := range r.Findings {
		if finding.Action != ActionNone {
			actionable = append(actionable, finding)
		}
	}
	return actionable
}
