//go:build darwin

package platform

import (
	"strings"
	"testing"
)

// A real sample of `pmset -g assertions` output, trimmed. Parsing is tested
// against a fixture rather than a live machine so that the parser has coverage
// even when the system tests do not run.
const pmsetSample = `Assertion status system-wide:
   BackgroundTask                 0
   ApplePushServiceTask           0
   UserIsActive                   1
   PreventUserIdleDisplaySleep    0
   PreventSystemSleep             0
   ExternalMedia                  0
   PreventUserIdleSystemSleep     1
   NetworkClientActive            0
Listed by owning process:
   pid 431(coreaudiod): [0x0000037200013a2f] 00:04:12 PreventUserIdleSystemSleep named: "com.apple.audio.context"
   pid 78321(caffeinate): [0x000003a100016b01] 00:00:02 PreventUserIdleSystemSleep named: "caffeinate command-line tool"
   pid 502(powerd): [0x0000000c00000a01] 12:00:00 InternalPreventDisplaySleep named: "com.apple.powerd"
`

func TestAssertionHeldBy(t *testing.T) {
	cases := []struct {
		name string
		pid  int
		want bool
	}{
		{"our mechanism", 78321, true},
		{"another process holding the same assertion", 431, true},
		{"a process holding a different assertion", 502, false},
		{"a process that is not listed", 99999, false},
		{"pid that is a prefix of a listed pid", 7832, false},
		{"pid that is a suffix of a listed pid", 8321, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := assertionHeldBy(pmsetSample, tc.pid); got != tc.want {
				t.Errorf("assertionHeldBy(pid %d) = %v, want %v", tc.pid, got, tc.want)
			}
		})
	}
}

// "Somebody is preventing sleep" is not the same claim as "Awake is preventing
// sleep". Verification must attribute the assertion to our own process.
func TestAssertionMustBeAttributable(t *testing.T) {
	withoutUs := strings.ReplaceAll(pmsetSample, "pid 78321(caffeinate)", "pid 431(coreaudiod)")

	if assertionHeldBy(withoutUs, 78321) {
		t.Error("verification passed on an assertion held by another process")
	}
}

func TestAssertionHeldByHandlesJunk(t *testing.T) {
	for _, output := range []string{"", "not pmset output at all", "pid (: PreventUserIdleSystemSleep"} {
		if assertionHeldBy(output, 78321) {
			t.Errorf("assertionHeldBy() accepted junk: %q", output)
		}
	}
}

// The real controller must report honestly about the machine it is on.
func TestDescribeOnRealMacOS(t *testing.T) {
	caps := New().Describe()

	if !caps.Available {
		t.Fatalf("caffeinate is unavailable on this macOS machine: %s", caps.Detail)
	}
	if caps.Mechanism != "caffeinate" {
		t.Errorf("mechanism = %q, want caffeinate", caps.Mechanism)
	}
	if caps.Path != caffeinatePath {
		t.Errorf("path = %q, want an absolute path", caps.Path)
	}
	if !caps.Supports(KindPreventIdleSleep) {
		t.Error("macOS does not report support for preventing idle sleep")
	}
	if !caps.CanVerify {
		t.Errorf("assertions cannot be verified on this machine: %s", caps.Detail)
	}
}

var _ Controller = (*macOS)(nil)
var _ Handle = (*macHandle)(nil)
