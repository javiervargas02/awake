package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var base = time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)

// serve returns a checker pointed at a local test server. No test touches the
// real network: a suite that fails on a plane is a broken suite.
func serve(t *testing.T, handler http.HandlerFunc) *Checker {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &Checker{URL: server.URL, Client: server.Client()}
}

func serveJSON(t *testing.T, body string) *Checker {
	t.Helper()
	return serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	})
}

const stableManifest = `{
  "schema_version": 1,
  "channels": {
    "stable": {
      "version": "0.2.0",
      "severity": "recommended",
      "released": "2026-09-01",
      "notes_url": "https://example.invalid/releases/v0.2.0"
    }
  }
}`

func TestParseVersion(t *testing.T) {
	cases := []struct {
		text string
		want Version
		ok   bool
	}{
		{"0.1.0", Version{Major: 0, Minor: 1, Patch: 0}, true},
		{"v1.2.3", Version{Major: 1, Minor: 2, Patch: 3}, true},
		{" 1.2.3 ", Version{Major: 1, Minor: 2, Patch: 3}, true},
		{"0.2.0-beta.1", Version{Major: 0, Minor: 2, PreRelease: "beta.1"}, true},
		{"1.2.3+build7", Version{Major: 1, Minor: 2, Patch: 3}, true},
		{"", Version{}, false},
		{"1.2", Version{}, false},
		{"1.2.3.4", Version{}, false},
		{"dev", Version{}, false},
		{"5c0b3ad-dirty", Version{}, false},
		{"1.2.x", Version{}, false},
		{"-1.0.0", Version{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got, err := ParseVersion(tc.text)
			if tc.ok != (err == nil) {
				t.Fatalf("ParseVersion(%q) error = %v, want ok=%v", tc.text, err, tc.ok)
			}
			if tc.ok && got != tc.want {
				t.Errorf("ParseVersion(%q) = %+v, want %+v", tc.text, got, tc.want)
			}
		})
	}
}

func TestVersionOrdering(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.0", 1},
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "0.1.1", -1},
		{"0.9.0", "1.0.0", -1},
		{"1.0.0", "1.0.0", 0},
		{"2.0.0", "10.0.0", -1}, // numeric, not lexicographic
		{"0.2.0-beta.1", "0.2.0", -1},
		{"0.2.0", "0.2.0-beta.1", 1},
		{"0.2.0-alpha", "0.2.0-beta", -1},
	}

	for _, tc := range cases {
		t.Run(tc.a+" vs "+tc.b, func(t *testing.T) {
			a, err := ParseVersion(tc.a)
			if err != nil {
				t.Fatalf("ParseVersion(%q): %v", tc.a, err)
			}
			b, err := ParseVersion(tc.b)
			if err != nil {
				t.Fatalf("ParseVersion(%q): %v", tc.b, err)
			}
			if got := a.Compare(b); got != tc.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestUpdateAvailable(t *testing.T) {
	checker := serveJSON(t, stableManifest)

	result := checker.Check(context.Background(), "0.1.0", DefaultChannel, base)

	if result.Outcome != OutcomeUpdateAvailable {
		t.Fatalf("outcome = %q, want %q (err: %v)", result.Outcome, OutcomeUpdateAvailable, result.Err)
	}
	if result.LatestVersion != "0.2.0" {
		t.Errorf("latest = %q, want 0.2.0", result.LatestVersion)
	}
	if result.Severity != SeverityRecommended {
		t.Errorf("severity = %q, want recommended", result.Severity)
	}
	if result.NotesURL == "" {
		t.Error("no notes URL was carried through")
	}
}

func TestUpToDate(t *testing.T) {
	checker := serveJSON(t, stableManifest)

	if got := checker.Check(context.Background(), "0.2.0", DefaultChannel, base); got.Outcome != OutcomeUpToDate {
		t.Errorf("outcome = %q, want %q", got.Outcome, OutcomeUpToDate)
	}
}

// A locally built binary newer than the manifest is normal, not anomalous.
func TestNewerThanManifestIsUpToDate(t *testing.T) {
	checker := serveJSON(t, stableManifest)

	if got := checker.Check(context.Background(), "0.9.0", DefaultChannel, base); got.Outcome != OutcomeUpToDate {
		t.Errorf("outcome = %q, want %q", got.Outcome, OutcomeUpToDate)
	}
}

// Development builds are stamped from git describe. Saying so beats guessing.
func TestDevelopmentBuildReportsUnknown(t *testing.T) {
	checker := serveJSON(t, stableManifest)

	result := checker.Check(context.Background(), "5c0b3ad-dirty", DefaultChannel, base)

	if result.Outcome != OutcomeUnknown {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeUnknown)
	}
	if result.LatestVersion != "0.2.0" {
		t.Errorf("latest = %q; the published version should still be reported", result.LatestVersion)
	}
}

func TestFailureModes(t *testing.T) {
	cases := []struct {
		name    string
		checker func(t *testing.T) *Checker
		want    Outcome
	}{
		{
			name: "not found",
			checker: func(t *testing.T) *Checker {
				return serve(t, func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "nope", http.StatusNotFound)
				})
			},
			want: OutcomeFailed,
		},
		{
			name: "server error",
			checker: func(t *testing.T) *Checker {
				return serve(t, func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "boom", http.StatusInternalServerError)
				})
			},
			want: OutcomeFailed,
		},
		{
			name:    "not json",
			checker: func(t *testing.T) *Checker { return serveJSON(t, "<html>hello</html>") },
			want:    OutcomeFailed,
		},
		{
			name:    "empty body",
			checker: func(t *testing.T) *Checker { return serveJSON(t, "") },
			want:    OutcomeFailed,
		},
		{
			name:    "no stable channel",
			checker: func(t *testing.T) *Checker { return serveJSON(t, `{"schema_version":1,"channels":{}}`) },
			want:    OutcomeFailed,
		},
		{
			name: "oversized body",
			checker: func(t *testing.T) *Checker {
				return serve(t, func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprint(w, `{"schema_version":1,"padding":"`)
					fmt.Fprint(w, strings.Repeat("x", maxBodyBytes+1))
					fmt.Fprint(w, `"}`)
				})
			},
			want: OutcomeFailed,
		},
		{
			name: "unreachable host",
			checker: func(t *testing.T) *Checker {
				server := httptest.NewServer(http.NotFoundHandler())
				client := server.Client()
				url := server.URL
				server.Close() // nothing is listening now
				return &Checker{URL: url, Client: client}
			},
			want: OutcomeFailed,
		},
		{
			name: "manifest schema from the future",
			checker: func(t *testing.T) *Checker {
				return serveJSON(t, `{"schema_version":99,"channels":{"stable":{"version":"9.0.0"}}}`)
			},
			want: OutcomeUnknown,
		},
		{
			name: "unusable version in the manifest",
			checker: func(t *testing.T) *Checker {
				return serveJSON(t, `{"schema_version":1,"channels":{"stable":{"version":"soon"}}}`)
			},
			want: OutcomeUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.checker(t).Check(context.Background(), "0.1.0", DefaultChannel, base)

			if result.Outcome != tc.want {
				t.Errorf("outcome = %q, want %q (err: %v)", result.Outcome, tc.want, result.Err)
			}
			if result.Outcome == OutcomeFailed && result.Err == nil {
				t.Error("a failed check gave no reason")
			}
		})
	}
}

// A redirect to plain HTTP is a downgrade, not something to chase.
func TestRefusesRedirectToHTTP(t *testing.T) {
	insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, stableManifest)
	}))
	t.Cleanup(insecure.Close)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, insecure.URL, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	// The default client's redirect policy is what is under test.
	checker := &Checker{URL: redirector.URL, Client: defaultClient()}

	result := checker.Check(context.Background(), "0.1.0", DefaultChannel, base)
	if result.Outcome != OutcomeFailed {
		t.Errorf("outcome = %q, want %q; an HTTP redirect must be refused", result.Outcome, OutcomeFailed)
	}
}

// The running version is never transmitted: sending it would let the host
// observe version distribution across users.
func TestRequestRevealsNothing(t *testing.T) {
	var seen *http.Request

	checker := serve(t, func(w http.ResponseWriter, r *http.Request) {
		clone := r.Clone(context.Background())
		seen = clone
		fmt.Fprint(w, stableManifest)
	})

	checker.Check(context.Background(), "0.1.0", DefaultChannel, base)

	if seen == nil {
		t.Fatal("no request was made")
	}
	if agent := seen.Header.Get("User-Agent"); agent != userAgent {
		t.Errorf("user agent = %q, want %q with no version", agent, userAgent)
	}
	if seen.URL.RawQuery != "" {
		t.Errorf("request carried a query string: %q", seen.URL.RawQuery)
	}
	for name, values := range seen.Header {
		for _, value := range values {
			if strings.Contains(value, "0.1.0") {
				t.Errorf("header %s leaks the running version: %q", name, value)
			}
		}
	}
}

func TestContextCancellationStopsTheCheck(t *testing.T) {
	checker := serve(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	result := checker.Check(ctx, "0.1.0", DefaultChannel, base)
	if result.Outcome != OutcomeFailed {
		t.Errorf("outcome = %q, want %q", result.Outcome, OutcomeFailed)
	}
}

// An unknown channel falls back to stable rather than failing.
func TestUnknownChannelFallsBackToStable(t *testing.T) {
	checker := serveJSON(t, stableManifest)

	result := checker.Check(context.Background(), "0.1.0", "nightly", base)

	if result.Outcome != OutcomeUpdateAvailable {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeUpdateAvailable)
	}
	if result.Channel != DefaultChannel {
		t.Errorf("channel = %q, want it to fall back to %q", result.Channel, DefaultChannel)
	}
}

func TestUnknownSeverityDoesNotDiscardTheAnswer(t *testing.T) {
	checker := serveJSON(t,
		`{"schema_version":1,"channels":{"stable":{"version":"0.2.0","severity":"apocalyptic"}}}`)

	result := checker.Check(context.Background(), "0.1.0", DefaultChannel, base)

	if result.Outcome != OutcomeUpdateAvailable {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeUpdateAvailable)
	}
	if result.Severity != SeverityOptional {
		t.Errorf("severity = %q, want it to degrade to optional", result.Severity)
	}
}

func TestCacheFreshness(t *testing.T) {
	cache := NewCache(Result{
		Outcome: OutcomeUpToDate, Channel: DefaultChannel, CheckedAt: base,
	})

	cases := []struct {
		name     string
		now      time.Time
		interval time.Duration
		fresh    bool
	}{
		{"just checked", base, 24 * time.Hour, true},
		{"within the interval", base.Add(23 * time.Hour), 24 * time.Hour, true},
		{"exactly at the interval", base.Add(24 * time.Hour), 24 * time.Hour, false},
		{"past the interval", base.Add(48 * time.Hour), 24 * time.Hour, false},
		{"clock moved backwards", base.Add(-time.Hour), 24 * time.Hour, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cache.Fresh(tc.now, tc.interval); got != tc.fresh {
				t.Errorf("Fresh() = %v, want %v", got, tc.fresh)
			}
		})
	}

	var never Cache
	if never.Fresh(base, time.Hour) {
		t.Error("an empty cache reported itself as fresh")
	}
}

// Failures are cached too, or an offline machine retries on every command.
func TestFailuresAreCacheable(t *testing.T) {
	cache := NewCache(Result{Outcome: OutcomeFailed, Channel: DefaultChannel, CheckedAt: base})

	if cache.Result != OutcomeFailed {
		t.Errorf("result = %q, want %q", cache.Result, OutcomeFailed)
	}
	if !cache.Fresh(base.Add(time.Minute), time.Hour) {
		t.Error("a cached failure was not treated as fresh")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	original := Result{
		Outcome: OutcomeUpdateAvailable, Channel: DefaultChannel,
		LatestVersion: "0.2.0", Severity: SeveritySecurity,
		NotesURL: "https://example.invalid/notes", CheckedAt: base,
	}

	restored := NewCache(original).AsResult("0.1.0")

	if restored.Outcome != original.Outcome ||
		restored.LatestVersion != original.LatestVersion ||
		restored.Severity != original.Severity ||
		restored.NotesURL != original.NotesURL {
		t.Errorf("round trip lost information: %+v", restored)
	}
	if !restored.FromCache {
		t.Error("a restored result does not report itself as cached")
	}
	if restored.CurrentVersion != "0.1.0" {
		t.Errorf("current version = %q", restored.CurrentVersion)
	}
}

// The manifest committed in this repository must be one this build can read.
func TestShippedManifestIsValid(t *testing.T) {
	checker := serveJSON(t, `{
  "schema_version": 1,
  "channels": {
    "stable": {
      "version": "0.1.0",
      "severity": "optional",
      "released": "2026-08-07",
      "notes_url": "https://github.com/javiervargas02/awake/releases/tag/v0.1.0"
    }
  }
}`)

	result := checker.Check(context.Background(), "0.1.0", DefaultChannel, base)

	if result.Outcome != OutcomeUpToDate {
		t.Errorf("outcome = %q, want %q (err: %v)", result.Outcome, OutcomeUpToDate, result.Err)
	}
	if result.Severity != SeverityOptional {
		t.Errorf("severity = %q", result.Severity)
	}
}
