package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/javiervargas02/awake/internal/update"
)

// updateOutput is the shape of `awake update check --json`.
type updateOutput struct {
	SchemaVersion  int    `json:"schema_version"`
	Result         string `json:"result"`
	Channel        string `json:"channel"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	Severity       string `json:"severity,omitempty"`
	NotesURL       string `json:"notes_url,omitempty"`
	CheckedAt      string `json:"checked_at"`
	FromCache      bool   `json:"from_cache"`
	Error          string `json:"error,omitempty"`
}

func runUpdate(ctx context.Context, args []string, deps Deps) error {
	if len(args) == 0 {
		return usagef("update needs a subcommand; try 'awake update check'")
	}

	switch args[0] {
	case "check":
		return runUpdateCheck(ctx, args[1:], deps)
	default:
		return usagef("unknown update subcommand %q; try 'awake update check'", args[0])
	}
}

func runUpdateCheck(ctx context.Context, args []string, deps Deps) error {
	var opts options
	var force bool

	fs := newFlagSet("update check", &opts)
	fs.BoolVar(&force, "force", false, "check now, ignoring the cached answer")

	operands, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(operands) > 0 {
		return usagef("update check takes no arguments")
	}

	svc, cleanup, err := service(deps, opts.verbose)
	if err != nil {
		return err
	}
	defer cleanup()

	svc.Started("update check")

	result, err := svc.CheckUpdate(ctx, force)
	if err != nil {
		return err
	}

	if opts.json {
		out := updateOutput{
			SchemaVersion:  1,
			Result:         result.Outcome.String(),
			Channel:        result.Channel,
			CurrentVersion: result.CurrentVersion,
			LatestVersion:  result.LatestVersion,
			NotesURL:       result.NotesURL,
			CheckedAt:      result.CheckedAt.UTC().Format(time.RFC3339),
			FromCache:      result.FromCache,
		}
		if result.Severity != "" {
			out.Severity = result.Severity.String()
		}
		if result.Err != nil {
			out.Error = result.Err.Error()
		}
		return writeJSON(deps.Stdout, out)
	}

	writeUpdateResult(deps, result)

	// Exit 0 in every case, including an unreachable network: "I could not
	// reach the host" is not a defect in Awake (ADR-0005).
	return nil
}

func writeUpdateResult(deps Deps, result update.Result) {
	switch result.Outcome {
	case update.OutcomeDisabled:
		fmt.Fprintln(deps.Stdout,
			"Update checking is disabled, so Awake made no network request.")
		fmt.Fprintln(deps.Stdout,
			"Set updates.enabled = true in ~/.awake/config.toml to turn it back on.")

	case update.OutcomeUpdateAvailable:
		fmt.Fprintf(deps.Stdout, "Awake %s is available. You have %s.\n",
			result.LatestVersion, result.CurrentVersion)
		if result.Severity == update.SeveritySecurity {
			fmt.Fprintln(deps.Stdout, "\nThis is a security release.")
		}
		if result.NotesURL != "" {
			fmt.Fprintf(deps.Stdout, "\nRelease notes: %s\n", result.NotesURL)
		}
		// Awake never installs. It says what is available and how to get it.
		fmt.Fprintln(deps.Stdout,
			"\nAwake does not install updates. Download it yourself when you are ready.")

	case update.OutcomeUpToDate:
		fmt.Fprintf(deps.Stdout, "Awake %s is the latest release.\n", result.CurrentVersion)

	case update.OutcomeUnknown:
		fmt.Fprintf(deps.Stdout, "Could not compare versions: %s\n", reason(result))
		if result.LatestVersion != "" {
			fmt.Fprintf(deps.Stdout, "The latest published release is %s.\n", result.LatestVersion)
		}

	default:
		fmt.Fprintf(deps.Stdout, "Could not check for updates: %s\n", reason(result))
		fmt.Fprintln(deps.Stdout, "This is not a problem with your installation.")
	}

	if result.FromCache {
		fmt.Fprintf(deps.Stdout, "\n(cached answer from %s)\n",
			result.CheckedAt.Local().Format("2006-01-02 15:04"))
	}
}

func reason(result update.Result) string {
	if result.Err == nil {
		return "no reason given"
	}
	// One sentence, lowercase, no trailing period: it is being embedded.
	return strings.TrimSuffix(result.Err.Error(), ".")
}
