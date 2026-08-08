package clock

import (
	"sync"
	"testing"
	"time"
)

func TestSystemReportsUTC(t *testing.T) {
	if loc := (System{}).Now().Location(); loc != time.UTC {
		t.Errorf("System.Now() location = %v, want UTC", loc)
	}
}

func TestFakeDoesNotMoveOnItsOwn(t *testing.T) {
	base := time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC)
	f := NewFake(base)

	first := f.Now()
	time.Sleep(2 * time.Millisecond)

	if !f.Now().Equal(first) {
		t.Error("Fake advanced without being told to")
	}

	f.Advance(90 * time.Minute)
	if want := base.Add(90 * time.Minute); !f.Now().Equal(want) {
		t.Errorf("after Advance: %v, want %v", f.Now(), want)
	}
}

func TestFakeNormalisesToUTC(t *testing.T) {
	elsewhere := time.Date(2026, 8, 7, 14, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60))
	f := NewFake(elsewhere)

	if loc := f.Now().Location(); loc != time.UTC {
		t.Errorf("Fake.Now() location = %v, want UTC", loc)
	}
}

// The fake is shared with concurrent code under the race detector, so it has
// to be safe for concurrent use.
func TestFakeIsConcurrencySafe(t *testing.T) {
	f := NewFake(time.Date(2026, 8, 7, 14, 0, 0, 0, time.UTC))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); f.Advance(time.Second) }()
		go func() { defer wg.Done(); _ = f.Now() }()
	}
	wg.Wait()

	if want := 50 * time.Second; !f.Now().Equal(time.Date(2026, 8, 7, 14, 0, 50, 0, time.UTC)) {
		t.Errorf("after %v of advances: %v", want, f.Now())
	}
}

// Clock is satisfied by both implementations. This fails at compile time if
// either ever drifts from the interface.
var (
	_ Clock = System{}
	_ Clock = (*Fake)(nil)
)
