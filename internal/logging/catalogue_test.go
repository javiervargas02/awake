package logging

import (
	"strings"
	"testing"
)

// TestCatalogueIsUnchanged is the schema contract test.
//
// The event vocabulary is public API (principle 8). The failure mode this
// guards against is not a bug — it is a rename nobody noticed was breaking. Any
// change here shows up as a diff in this list, which is the prompt to write the
// changelog entry and decide whether SchemaVersion must increment.
func TestCatalogueIsUnchanged(t *testing.T) {
	want := []string{
		"app.started",
		"config.loaded",
		"config.defaulted",
		"config.unknown_key",
		"session.start_refused",
		"session.created",
		"session.started",
		"session.completed",
		"session.stopped",
		"session.failed",
		"session.recovered",
		"mode.started",
		"mode.stopped",
		"mode.failed",
		"update.check.started",
		"update.check.completed",
		"update.available",
		"health.check.completed",
		"repair.performed",
		"log.sink_failed",
	}

	got := make([]string, 0, len(Catalogue()))
	for _, spec := range Catalogue() {
		got = append(got, spec.Name)
	}

	if len(got) != len(want) {
		t.Fatalf("catalogue has %d events, want %d:\n got %v\nwant %v",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSchemaVersionIsOne(t *testing.T) {
	// A change here is a breaking change to a published contract. If this test
	// fails, that decision is being made — make it deliberately.
	if SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d; changing it is a major-version decision", SchemaVersion)
	}
}

func TestEventNamesAreNamespaced(t *testing.T) {
	for _, spec := range Catalogue() {
		t.Run(spec.Name, func(t *testing.T) {
			if !strings.Contains(spec.Name, ".") {
				t.Errorf("event %q is not namespaced by subsystem", spec.Name)
			}
			if spec.Name != strings.ToLower(spec.Name) {
				t.Errorf("event %q is not lowercase", spec.Name)
			}
			if strings.ContainsAny(spec.Name, " -") {
				t.Errorf("event %q contains a space or hyphen", spec.Name)
			}
		})
	}
}

func TestCatalogueHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range Catalogue() {
		if seen[spec.Name] {
			t.Errorf("duplicate event %q", spec.Name)
		}
		seen[spec.Name] = true
	}
}

func TestSpecFieldsAreDistinct(t *testing.T) {
	for _, spec := range Catalogue() {
		t.Run(spec.Name, func(t *testing.T) {
			seen := map[string]bool{}
			for _, field := range append(append([]string{}, spec.Required...), spec.Optional...) {
				if seen[field] {
					t.Errorf("field %q appears twice", field)
				}
				seen[field] = true
			}
		})
	}
}

func TestSpecFor(t *testing.T) {
	spec, ok := SpecFor(EventSessionCompleted)
	if !ok {
		t.Fatalf("SpecFor(%q) not found", EventSessionCompleted)
	}
	if !spec.SessionScoped {
		t.Error("session.completed is not marked session-scoped")
	}

	if _, ok := SpecFor("session.invented"); ok {
		t.Error("SpecFor() found an event that does not exist")
	}
}

// session.start_refused has no session ID, because no session was created. It
// is logged because a refusal is a meaningful action.
func TestStartRefusedIsNotSessionScoped(t *testing.T) {
	spec, ok := SpecFor(EventSessionStartRefused)
	if !ok {
		t.Fatal("session.start_refused is missing from the catalogue")
	}
	if spec.SessionScoped {
		t.Error("session.start_refused is marked session-scoped, but no session exists when it is emitted")
	}
}
