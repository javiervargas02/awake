package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestExitCodes asserts the public exit-code contract
// (docs/architecture/cli-contract.md). It is a table-driven test: one case per
// row, each run as its own named subtest.
func TestExitCodes(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{name: "no arguments prints help", args: nil, want: ExitOK},
		{name: "help flag", args: []string{"--help"}, want: ExitOK},
		{name: "version", args: []string{"version"}, want: ExitOK},
		{name: "version json", args: []string{"version", "--json"}, want: ExitOK},
		{name: "unknown command", args: []string{"nope"}, want: ExitUsage},
		{name: "unknown flag", args: []string{"--nope"}, want: ExitUsage},
		{name: "unknown flag on command", args: []string{"version", "--nope"}, want: ExitUsage},
		{name: "unexpected argument", args: []string{"version", "extra"}, want: ExitUsage},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			got := Run(tc.args, &out, &errOut)
			if got != tc.want {
				t.Errorf("Run(%q) = %d, want %d (stderr: %s)",
					tc.args, got, tc.want, errOut.String())
			}
		})
	}
}

// TestErrorsGoToStderr guards the rule that stdout is a data channel: a failed
// command must not write anything a script would have to filter out.
func TestErrorsGoToStderr(t *testing.T) {
	var out, errOut bytes.Buffer

	if code := Run([]string{"nope"}, &out, &errOut); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want empty", out.String())
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("stderr = %q, want it to explain the error", errOut.String())
	}
}

// TestVersionJSONShape is a contract test for the --json output. It fails if a
// field is renamed or removed, which is the prompt to write a changelog entry
// and consider whether schema_version must increment.
func TestVersionJSONShape(t *testing.T) {
	var out, errOut bytes.Buffer

	if code := Run([]string{"version", "--json"}, &out, &errOut); code != ExitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, ExitOK, errOut.String())
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	want := []string{"schema_version", "version", "commit", "built", "go_version", "platform"}
	for _, field := range want {
		if _, ok := got[field]; !ok {
			t.Errorf("missing field %q in %v", field, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d fields, want %d: %v", len(got), len(want), got)
	}
}

// TestVersionNeverEmpty asserts that a binary built without linker stamps still
// identifies itself honestly rather than printing blanks.
func TestVersionNeverEmpty(t *testing.T) {
	var out, errOut bytes.Buffer

	if code := Run([]string{"version"}, &out, &errOut); code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	line := out.String()
	for _, unwanted := range []string{"()", "  ", "commit ,"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("version line has an empty field: %q", line)
		}
	}
}
