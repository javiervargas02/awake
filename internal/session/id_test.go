package session

import (
	"sort"
	"testing"
	"time"
)

func TestNewIDIsValidAndUnique(t *testing.T) {
	now := time.Date(2026, 8, 7, 14, 3, 11, 0, time.UTC)

	seen := make(map[ID]bool)
	for i := 0; i < 1000; i++ {
		id, err := NewID(now)
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
		if !id.Valid() {
			t.Fatalf("NewID() produced an invalid id: %q", id)
		}
		if seen[id] {
			t.Fatalf("NewID() produced a duplicate: %q", id)
		}
		seen[id] = true
	}
}

// Sortability is the property that makes `ls` on the session log directory
// list sessions oldest-first without any tooling.
func TestIDsSortChronologically(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var ids []string
	var chronological []string
	for i := 0; i < 50; i++ {
		id, err := NewID(start.Add(time.Duration(i) * time.Hour))
		if err != nil {
			t.Fatalf("NewID() error = %v", err)
		}
		ids = append(ids, id.String())
		chronological = append(chronological, id.String())
	}

	sort.Strings(ids)

	for i := range ids {
		if ids[i] != chronological[i] {
			t.Fatalf("lexicographic order differs from chronological order at %d:\n got %s\nwant %s",
				i, ids[i], chronological[i])
		}
	}
}

func TestIDUsesUTC(t *testing.T) {
	// Same instant, different zone: the ID must not change.
	utc := time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)
	elsewhere := utc.In(time.FixedZone("UTC-5", -5*60*60))

	a, err := NewID(utc)
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}
	b, err := NewID(elsewhere)
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}

	if a[:16] != b[:16] {
		t.Errorf("timestamp differs by zone: %q vs %q", a, b)
	}
}

func TestIDValidRejectsMalformed(t *testing.T) {
	cases := []ID{
		"",
		"not-an-id",
		"20260807t140311z",                 // missing suffix
		"20260807t140311z-",                // empty suffix
		"20260807t140311z-tooshort",        // wrong suffix length
		"20260807t140311z-k3m9x2q7r1extra", // too long
		"20260899t140311z-k3m9x2q7r1",      // impossible date
		"20260807t140311z-k3m9x2q7ra",      // 'a' is not in the alphabet
	}

	for _, id := range cases {
		t.Run(string(id), func(t *testing.T) {
			if id.Valid() {
				t.Errorf("Valid() accepted malformed id %q", id)
			}
		})
	}
}

func TestIDIsFilesystemSafe(t *testing.T) {
	id, err := NewID(time.Now())
	if err != nil {
		t.Fatalf("NewID() error = %v", err)
	}

	for _, r := range id.String() {
		safe := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || r == '-'
		if !safe {
			t.Errorf("id %q contains character %q, which is not filename-safe", id, r)
		}
	}
}
