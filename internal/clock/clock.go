// Package clock makes time an injectable dependency.
//
// ADR-0002 makes an absolute wall-clock deadline authoritative for a session,
// which means deadline behaviour — including the machine-slept-past-its-end
// case — is only testable if nothing reads the system clock directly.
// Everything in Awake that needs the time asks a Clock for it.
package clock

import (
	"sync"
	"time"
)

// Clock reports the current time and lets callers wait.
//
// Waiting belongs here for the same reason reading the time does: a session
// that waits for its deadline is the behaviour most worth testing, and a test
// that waits thirty real minutes is not a test.
type Clock interface {
	Now() time.Time

	// After delivers one value once the duration has elapsed.
	After(d time.Duration) <-chan time.Time
}

// System is the real clock. It reports UTC, because every timestamp Awake
// stores or logs is UTC (see the logging architecture).
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

func (System) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Fake is a manually advanced clock for tests.
//
// It is safe for concurrent use so that it can be shared with code under the
// race detector, which the testing strategy runs on every commit.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

// NewFake returns a Fake set to the given instant, normalised to UTC.
func NewFake(now time.Time) *Fake {
	return &Fake{now: now.UTC()}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// After registers a timer that fires when the fake clock reaches the deadline.
// A duration that has already elapsed fires immediately, which is what makes
// "the machine woke up past its deadline" straightforward to express.
func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	ch := make(chan time.Time, 1)
	timer := &fakeTimer{at: f.now.Add(d), ch: ch}

	if !timer.at.After(f.now) {
		ch <- f.now
		return ch
	}

	f.timers = append(f.timers, timer)
	return ch
}

// Advance moves the clock forward and fires any timer that has come due.
// Advancing by a large amount is how tests simulate a machine having slept
// through a deadline.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setLocked(f.now.Add(d))
}

// Set moves the clock to an absolute instant, forwards or backwards.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setLocked(t.UTC())
}

func (f *Fake) setLocked(t time.Time) {
	f.now = t

	pending := f.timers[:0]
	for _, timer := range f.timers {
		if timer.at.After(f.now) {
			pending = append(pending, timer)
			continue
		}
		select {
		case timer.ch <- f.now:
		default:
		}
	}
	f.timers = pending
}
