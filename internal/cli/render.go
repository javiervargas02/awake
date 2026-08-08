package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/javiervargas02/awake/internal/logging"
	"github.com/javiervargas02/awake/internal/session"
)

// sessionOutput is the machine-readable shape of a session.
//
// It is public API (principle 8), and it is the supported way to read Awake's
// state programmatically: the files under ~/.awake are an implementation
// detail. Adding a field is additive; renaming or removing one is breaking.
//
// Timestamps use the same layout as the log envelope, so a time seen here can
// be matched against the logs without conversion.
type sessionOutput struct {
	ID         string  `json:"id"`
	Mode       string  `json:"mode"`
	Status     string  `json:"status"`
	EndReason  *string `json:"end_reason"`
	Indefinite bool    `json:"indefinite"`

	RequestedDuration *string `json:"requested_duration"`
	Deadline          *string `json:"deadline"`
	StartedAt         string  `json:"started_at"`
	EndedAt           *string `json:"ended_at"`

	// Remaining is null for an indefinite session, which has no remaining
	// time, and for one that has ended. A sentinel number would be a lie.
	Remaining *string `json:"remaining"`
	Elapsed   string  `json:"elapsed"`
}

func describeSession(sess *session.Session, now time.Time) *sessionOutput {
	if sess == nil {
		return nil
	}

	out := &sessionOutput{
		ID:         sess.ID.String(),
		Mode:       sess.Mode.String(),
		Status:     sess.Status.String(),
		Indefinite: sess.Indefinite,
		StartedAt:  sess.StartedAt.Format(logging.TimestampLayout),
		Elapsed:    sess.Elapsed(now).String(),
	}

	if sess.EndReason != "" {
		reason := sess.EndReason.String()
		out.EndReason = &reason
	}
	if !sess.Indefinite {
		duration := sess.RequestedDuration.String()
		out.RequestedDuration = &duration
	}
	if sess.Deadline != nil {
		deadline := sess.Deadline.Format(logging.TimestampLayout)
		out.Deadline = &deadline
	}
	if sess.EndedAt != nil {
		ended := sess.EndedAt.Format(logging.TimestampLayout)
		out.EndedAt = &ended
	}
	if !sess.Status.IsTerminal() {
		if remaining, ok := sess.Remaining(now); ok {
			text := remaining.String()
			out.Remaining = &text
		}
	}

	return out
}

// writeJSON emits one indented JSON object followed by a newline: readable in
// a terminal, and still one object per line when piped.
func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// humanTime renders an instant in the user's local timezone. Machine output
// stays UTC; only people get local time.
func humanTime(t time.Time) string { return t.Local().Format("15:04:05") }

// humanDuration rounds to something a person would say out loud.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return d.Round(time.Second).String()
	default:
		return d.Round(time.Minute).String()
	}
}

// describeEnding turns a terminal session into one plain sentence.
func describeEnding(sess *session.Session, now time.Time) string {
	elapsed := humanDuration(sess.Elapsed(now))

	switch sess.EndReason {
	case session.ReasonDurationElapsed:
		if overrun := sess.Overrun(now); overrun > time.Minute {
			// The machine slept through the deadline. Saying so is what makes
			// a late ending explainable instead of suspicious.
			return fmt.Sprintf("Session completed after %s (its deadline passed %s ago, while the machine was asleep).",
				elapsed, humanDuration(overrun))
		}
		return fmt.Sprintf("Session completed after %s.", elapsed)

	case session.ReasonUserStopped:
		return fmt.Sprintf("Session stopped after %s.", elapsed)

	case session.ReasonInterrupted:
		return fmt.Sprintf("Session interrupted after %s.", elapsed)

	case session.ReasonModeFailure:
		return fmt.Sprintf("Session failed after %s: this computer is no longer being kept awake.", elapsed)

	case session.ReasonCrashed:
		return fmt.Sprintf("Session ended unexpectedly after %s.", elapsed)

	default:
		return fmt.Sprintf("Session ended after %s.", elapsed)
	}
}
