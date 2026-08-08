package cli

import (
	"encoding/json"
	"fmt"
	"io"

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

func runVersion(args []string, out, _ io.Writer) error {
	var opts options
	fs := newFlagSet("version", &opts)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usagef("version takes no arguments")
	}

	info := buildinfo.Get()

	if opts.json {
		return writeJSON(out, versionOutput{SchemaVersion: 1, Info: info})
	}

	fmt.Fprintf(out, "awake %s (commit %s, built %s, %s, %s)\n",
		info.Version, info.Commit, info.Built, info.GoVersion, info.Platform)
	return nil
}

// writeJSON emits one indented JSON object followed by a newline, so that
// output is readable in a terminal and still parses cleanly when piped.
func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
