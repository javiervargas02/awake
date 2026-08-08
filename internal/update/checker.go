package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ManifestURL is compiled into the binary and is deliberately not
// configurable (ADR-0009): a user-settable update URL would be a
// supply-chain hole, since anything able to edit config.toml could point
// Awake at an attacker's manifest.
const ManifestURL = "https://javiervargas02.github.io/awake/updates/manifest.json"

const (
	// requestTimeout keeps a check from ever feeling like a hang.
	requestTimeout = 5 * time.Second

	// maxBodyBytes stops a hostile or broken host exhausting memory. The real
	// manifest is a few hundred bytes.
	maxBodyBytes = 64 << 10

	// maxRedirects bounds redirect chasing.
	maxRedirects = 3

	// userAgent identifies the client and carries no version: sending the
	// running version would let the host observe version distribution across
	// users, which is telemetry by another name.
	userAgent = "awake"
)

// Outcome is the result of a check.
type Outcome string

const (
	// OutcomeUpToDate: this build is current, or newer than the manifest.
	OutcomeUpToDate Outcome = "up_to_date"
	// OutcomeUpdateAvailable: a newer release exists.
	OutcomeUpdateAvailable Outcome = "update_available"
	// OutcomeFailed: the manifest could not be fetched or understood. Not an
	// error the user must act on — being offline is not a defect in Awake.
	OutcomeFailed Outcome = "failed"
	// OutcomeUnknown: the running build has no comparable version, or the
	// manifest schema is newer than this build understands.
	OutcomeUnknown Outcome = "unknown"
	// OutcomeDisabled: the user turned checking off, so no request was made.
	OutcomeDisabled Outcome = "disabled"
)

func (o Outcome) String() string { return string(o) }

// Result is what a check concluded.
type Result struct {
	Outcome        Outcome
	Channel        string
	CurrentVersion string
	LatestVersion  string
	Severity       Severity
	NotesURL       string

	// CheckedAt is when the answer was obtained — which may be earlier than
	// now, if it came from the cache.
	CheckedAt time.Time

	// FromCache reports whether the network was consulted.
	FromCache bool

	// Err explains a failed check. It is informational: no caller treats it as
	// a reason to fail a command.
	Err error
}

// Available reports whether the user should be told about a new release.
func (r Result) Available() bool { return r.Outcome == OutcomeUpdateAvailable }

// Checker fetches and interprets the manifest.
type Checker struct {
	// URL defaults to ManifestURL. Tests inject a local server here — through
	// the internal API, never through config.
	URL string

	// Client defaults to one with a short timeout and an HTTPS-only redirect
	// policy.
	Client *http.Client
}

func NewChecker() *Checker {
	return &Checker{URL: ManifestURL, Client: defaultClient()}
}

// defaultClient refuses to follow a redirect to plain HTTP: a downgrade is a
// tampering signal, not something to chase.
func defaultClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("refusing to follow a redirect to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// Check fetches the manifest and compares it against the running version.
//
// It never returns an error: every failure is an Outcome, because a failed
// update check is a logged non-event rather than something a command should
// fail over.
func (c *Checker) Check(ctx context.Context, currentVersion, channel string, now time.Time) Result {
	if channel == "" {
		channel = DefaultChannel
	}

	result := Result{
		Channel:        channel,
		CurrentVersion: currentVersion,
		CheckedAt:      now,
		Outcome:        OutcomeFailed,
	}

	manifest, err := c.fetch(ctx)
	if err != nil {
		result.Err = err
		return result
	}

	release, resolved, err := manifest.Channel(channel)
	if err != nil {
		result.Err = err
		result.Channel = resolved
		if errors.Is(err, ErrUnknownSchema) {
			result.Outcome = OutcomeUnknown
		}
		return result
	}

	result.Channel = resolved
	result.LatestVersion = release.Version
	result.NotesURL = release.NotesURL
	if release.Severity.Valid() {
		result.Severity = release.Severity
	} else {
		// An unrecognised severity from a newer publisher is not a reason to
		// discard an otherwise good answer.
		result.Severity = SeverityOptional
	}

	latest, err := ParseVersion(release.Version)
	if err != nil {
		result.Outcome = OutcomeUnknown
		result.Err = fmt.Errorf("manifest version is unusable: %w", err)
		return result
	}

	current, err := ParseVersion(currentVersion)
	if err != nil {
		// A development build stamped from git describe. There is nothing to
		// compare, and saying so is better than guessing.
		result.Outcome = OutcomeUnknown
		result.Err = fmt.Errorf("this build has no comparable version: %w", err)
		return result
	}

	if current.OlderThan(latest) {
		result.Outcome = OutcomeUpdateAvailable
	} else {
		// Includes the case of a locally built binary newer than the manifest,
		// which is normal rather than anomalous.
		result.Outcome = OutcomeUpToDate
	}
	return result
}

func (c *Checker) fetch(ctx context.Context) (Manifest, error) {
	url := c.URL
	if url == "" {
		url = ManifestURL
	}
	client := c.Client
	if client == nil {
		client = defaultClient()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Manifest{}, fmt.Errorf("building the request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return Manifest{}, fmt.Errorf("contacting the update host: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("update host returned %s", response.Status)
	}

	// Read one byte more than the limit so an oversized body is detected
	// rather than silently truncated into something that might still parse.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("reading the manifest: %w", err)
	}
	if len(body) > maxBodyBytes {
		return Manifest{}, fmt.Errorf("manifest is larger than %d bytes", maxBodyBytes)
	}

	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("manifest is not valid JSON: %w", err)
	}
	return manifest, nil
}
