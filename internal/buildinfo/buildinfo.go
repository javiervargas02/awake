// Package buildinfo reports the identity of this binary.
//
// Principle 4 requires that critical truth live in the binary rather than in
// user-editable files: nothing under ~/.awake can change what version Awake
// reports itself to be. These values are stamped in at link time by the build
// (see the Makefile), with a fallback to the metadata the Go toolchain embeds
// automatically so that `go install` and `go run` builds still identify
// themselves honestly.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

// Stamped at link time via -ldflags "-X". Unexported so that nothing but this
// package can pretend to be a different version.
var (
	version string
	commit  string
	date    string
)

// Info describes the running binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Built     string `json:"built"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Get returns the identity of this binary. It never fails and never returns
// empty fields: unknown values are reported as "dev" or "unknown" rather than
// as blanks, so that output is always readable and never ambiguous.
func Get() Info {
	i := Info{
		Version:   version,
		Commit:    commit,
		Built:     date,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	if i.Version == "" || i.Commit == "" || i.Built == "" {
		fillFromEmbedded(&i)
	}

	if i.Version == "" {
		i.Version = "dev"
	}
	if i.Commit == "" {
		i.Commit = "unknown"
	}
	if i.Built == "" {
		i.Built = "unknown"
	}
	return i
}

// fillFromEmbedded consults the build metadata the Go toolchain records in
// every binary. This covers builds that did not go through the Makefile.
func fillFromEmbedded(i *Info) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	if i.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		i.Version = bi.Main.Version
	}

	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			if i.Commit == "" {
				i.Commit = setting.Value
			}
		case "vcs.time":
			if i.Built == "" {
				i.Built = setting.Value
			}
		}
	}
}
