package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a semantic version.
//
// Awake compares versions locally rather than asking a server which is newer:
// the current version is never transmitted (ADR-0009), so the comparison has
// to happen here.
type Version struct {
	Major, Minor, Patch int

	// PreRelease sorts *before* the same version without one, so 0.2.0-beta.1
	// is older than 0.2.0.
	PreRelease string
}

// ParseVersion reads a semantic version, with or without a leading "v".
//
// It deliberately fails on anything else. Development builds are stamped from
// `git describe` and look like "5c0b3ad-dirty"; there is nothing sensible to
// compare, and guessing would be worse than saying so.
func ParseVersion(text string) (Version, error) {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "v")

	if trimmed == "" {
		return Version{}, fmt.Errorf("empty version")
	}

	// Build metadata carries no ordering, so it is discarded.
	if plus := strings.IndexByte(trimmed, '+'); plus >= 0 {
		trimmed = trimmed[:plus]
	}

	var pre string
	if dash := strings.IndexByte(trimmed, '-'); dash >= 0 {
		pre = trimmed[dash+1:]
		trimmed = trimmed[:dash]
	}

	fields := strings.Split(trimmed, ".")
	if len(fields) != 3 {
		return Version{}, fmt.Errorf("%q is not a semantic version", text)
	}

	numbers := make([]int, 3)
	for i, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil || value < 0 {
			return Version{}, fmt.Errorf("%q is not a semantic version", text)
		}
		numbers[i] = value
	}

	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2], PreRelease: pre}, nil
}

func (v Version) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.PreRelease != "" {
		return base + "-" + v.PreRelease
	}
	return base
}

// Compare reports whether v is older (-1), the same as (0), or newer (+1)
// than other.
func (v Version) Compare(other Version) int {
	for _, pair := range [][2]int{
		{v.Major, other.Major},
		{v.Minor, other.Minor},
		{v.Patch, other.Patch},
	} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return 1
		}
	}

	switch {
	case v.PreRelease == other.PreRelease:
		return 0
	case v.PreRelease == "":
		// A release is newer than any pre-release of the same version.
		return 1
	case other.PreRelease == "":
		return -1
	case v.PreRelease < other.PreRelease:
		return -1
	default:
		return 1
	}
}

func (v Version) OlderThan(other Version) bool { return v.Compare(other) < 0 }
