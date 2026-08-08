package cli

import (
	"context"
	"fmt"

	"github.com/javiervargas02/awake/internal/buildinfo"
)

// versionOutput is the --json shape for `awake version`.
//
// It is public API (principle 8): adding a field is additive, renaming or
// removing one is breaking. schema_version lets a consumer detect a format it
// does not understand instead of misreading it.
type versionOutput struct {
	SchemaVersion int `json:"schema_version"`
	buildinfo.Info
}

// runVersion reports the identity compiled into this binary.
//
// It reads no state and creates nothing: the version lives in the binary, not
// in a file a user could edit (principle 4), so asking a fresh install what it
// is leaves no trace on disk.
func runVersion(_ context.Context, args []string, deps Deps) error {
	var opts options

	fs := newFlagSet("version", &opts)
	operands, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(operands) > 0 {
		return usagef("version takes no arguments")
	}

	info := deps.Version

	if opts.json {
		return writeJSON(deps.Stdout, versionOutput{SchemaVersion: 1, Info: info})
	}

	fmt.Fprintf(deps.Stdout, "awake %s (commit %s, built %s, %s, %s)\n",
		info.Version, info.Commit, info.Built, info.GoVersion, info.Platform)
	return nil
}
