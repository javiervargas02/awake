package lock

import "sync"

// Fake is an in-process lock for tests.
type Fake struct {
	mu sync.Mutex

	// HeldByAnother makes TryAcquire report that someone else has the lock.
	HeldByAnother bool

	// Err makes TryAcquire report that it could not even ask, which is the
	// degraded case ADR-0008 requires Awake to survive.
	Err error

	held     bool
	acquires int
	releases int
}

func NewFake() *Fake { return &Fake{} }

func (f *Fake) Path() string { return "/fake/session.lock" }

func (f *Fake) TryAcquire() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.Err != nil {
		return false, f.Err
	}
	if f.HeldByAnother || f.held {
		return false, nil
	}

	f.held = true
	f.acquires++
	return true, nil
}

func (f *Fake) Release() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.held {
		f.releases++
	}
	f.held = false
	return nil
}

func (f *Fake) Held() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held
}

func (f *Fake) Releases() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases
}
