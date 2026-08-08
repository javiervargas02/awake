package update

import "time"

// CacheVersion is the on-disk format version of update.json.
//
// Like the session record, the cache is not public API: it exists for our own
// forward-compatibility. `awake update check --json` is the supported way to
// read this information.
const CacheVersion = 1

// Cache is the last answer and when it was obtained.
//
// It is a cache in the strict sense: nothing depends on it, and discarding it
// costs one network request.
type Cache struct {
	Version       int       `json:"version"`
	Channel       string    `json:"channel"`
	CheckedAt     time.Time `json:"checked_at"`
	Result        Outcome   `json:"result"`
	LatestVersion string    `json:"latest_version,omitempty"`
	Severity      Severity  `json:"severity,omitempty"`
	NotesURL      string    `json:"notes_url,omitempty"`
}

// NewCache records a result.
//
// Failures are cached too. Without that, an offline machine would retry on
// every single command — wasteful, and the opposite of what the interval is
// for.
func NewCache(result Result) Cache {
	return Cache{
		Version:       CacheVersion,
		Channel:       result.Channel,
		CheckedAt:     result.CheckedAt.UTC(),
		Result:        result.Outcome,
		LatestVersion: result.LatestVersion,
		Severity:      result.Severity,
		NotesURL:      result.NotesURL,
	}
}

// Fresh reports whether the cached answer is recent enough to reuse, which is
// what keeps a CLI command from ever waiting on a network round trip.
func (c Cache) Fresh(now time.Time, interval time.Duration) bool {
	if c.CheckedAt.IsZero() || interval <= 0 {
		return false
	}
	age := now.UTC().Sub(c.CheckedAt)
	return age >= 0 && age < interval
}

// Age reports how long ago the check happened, so that a stale answer can look
// stale rather than implying freshness.
func (c Cache) Age(now time.Time) time.Duration {
	if c.CheckedAt.IsZero() {
		return 0
	}
	return now.UTC().Sub(c.CheckedAt)
}

// Result reconstructs a result from the cache, for reporting without a
// network request.
func (c Cache) AsResult(currentVersion string) Result {
	return Result{
		Outcome:        c.Result,
		Channel:        c.Channel,
		CurrentVersion: currentVersion,
		LatestVersion:  c.LatestVersion,
		Severity:       c.Severity,
		NotesURL:       c.NotesURL,
		CheckedAt:      c.CheckedAt,
		FromCache:      true,
	}
}
