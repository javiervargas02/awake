package logging

import (
	"bytes"
	"strings"
	"testing"

	"github.com/javiervargas02/awake/internal/clock"
)

// Principle 5 is absolute, so it gets mechanical enforcement rather than review
// discipline.
//
// The allowlist is derived from the catalogue: a field cannot reach a log file
// unless it was declared first. This inverts the usual burden — a denylist asks
// us to imagine every way private data could leak, whereas an allowlist
// requires a deliberate decision before anything new is ever written.
func TestOnlyDeclaredFieldsAreLogged(t *testing.T) {
	allowedEnvelope := toSet(EnvelopeFields())

	for _, spec := range Catalogue() {
		t.Run(spec.Name, func(t *testing.T) {
			var global, trace bytes.Buffer

			logger := New(Options{Clock: clock.NewFake(base), Global: &global})
			if spec.SessionScoped {
				logger = logger.WithSession("20260807t140311z-k3m9x2q7r1", &trace)
			}

			data := Fields{}
			for _, field := range append(append([]string{}, spec.Required...), spec.Optional...) {
				data[field] = "value"
			}
			logger.Log(spec.Level, spec.Name, data)

			allowedData := toSet(append(append([]string{}, spec.Required...), spec.Optional...))

			for _, event := range decodeLines(t, global.String()) {
				for key := range event {
					if !allowedEnvelope[key] {
						t.Errorf("undeclared envelope field %q", key)
					}
				}
				payload, ok := event["data"].(map[string]any)
				if !ok {
					continue
				}
				for key := range payload {
					if !allowedData[key] {
						t.Errorf("undeclared data field %q on %s", key, spec.Name)
					}
				}
			}
		})
	}
}

// The catalogue itself must not name anything private. This catches a field
// added with good intentions and a bad name, before it is ever populated.
func TestCatalogueNamesNothingPrivate(t *testing.T) {
	forbidden := []string{
		"key_code", "keystroke", "keys", "clipboard", "screenshot", "screen",
		"window", "window_title", "title", "app_name", "application",
		"username", "user", "home", "hostname", "host", "ip", "mac_address",
		"env", "environment", "argv", "args", "arguments", "cmdline",
		"password", "token", "secret", "url", "path", "file", "filename",
	}

	for _, spec := range Catalogue() {
		fields := append(append([]string{}, spec.Required...), spec.Optional...)
		for _, field := range fields {
			for _, bad := range forbidden {
				if field == bad {
					t.Errorf("event %q declares field %q, which risks logging user activity",
						spec.Name, field)
				}
			}
		}
	}
}

// Raw command-line arguments are the most plausible route for arbitrary user
// text to reach a log file, so app.started records the resolved subcommand and
// nothing else.
func TestAppStartedRecordsOnlyTheSubcommand(t *testing.T) {
	spec, ok := SpecFor(EventAppStarted)
	if !ok {
		t.Fatal("app.started is missing from the catalogue")
	}

	fields := append(append([]string{}, spec.Required...), spec.Optional...)
	for _, field := range fields {
		if strings.Contains(field, "arg") || field == "cmdline" || field == "raw" {
			t.Errorf("app.started declares %q; raw arguments must never be logged", field)
		}
	}

	if !contains(fields, "command") {
		t.Error("app.started does not record which command ran")
	}
}

// When end-on-input lands (v0.2), it may record only *that* input occurred —
// never the device, the key, or the coordinates.
//
// The check is scoped to session and mode events, which is where input
// detection will live. Elsewhere "key" is an unrelated and legitimate word: a
// configuration key is not a keyboard key.
func TestInputDetectionCarriesNoDetail(t *testing.T) {
	for _, spec := range Catalogue() {
		if !strings.HasPrefix(spec.Name, "session.") && !strings.HasPrefix(spec.Name, "mode.") {
			continue
		}

		fields := append(append([]string{}, spec.Required...), spec.Optional...)
		for _, field := range fields {
			switch field {
			case "input_device", "input_type", "key", "key_code", "button",
				"coordinates", "position", "modifiers":
				t.Errorf("event %q declares %q; input detection may record only that input occurred",
					spec.Name, field)
			}
		}
	}
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
