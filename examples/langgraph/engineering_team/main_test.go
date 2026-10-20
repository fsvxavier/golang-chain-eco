package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

// Many goroutines race for the four engineers: exactly four may win, and
// none may win the same person twice.
func TestRegistryClaimIsAtomic(t *testing.T) {
	reg := newRegistry()
	var wins atomic.Int32
	got := sync.Map{}
	var wg sync.WaitGroup
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m := reg.claim([]string{"junior", "mid", "senior"}, "T"+string(rune('A'+i%26))); m != nil {
				wins.Add(1)
				if _, dup := got.LoadOrStore(m.id, true); dup {
					t.Errorf("%s reserved twice", m.id)
				}
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 4 {
		t.Fatalf("got %d reservations, want 4", wins.Load())
	}
}

// A queued high-complexity task never goes to a junior, and is assigned once
// a suitable engineer is released.
func TestBacklogWaitsForSuitableEngineer(t *testing.T) {
	reg := newRegistry()
	reg.claim([]string{"senior"}, "x")
	reg.claim([]string{"mid"}, "y")
	reg.enqueue(work{ID: "T9", Complexity: "high"})
	if _, ok := reg.next(); ok {
		t.Fatal("high task must not go to a junior")
	}
	reg.release("senior-1")
	if w, ok := reg.next(); !ok || w.Assignee != "senior-1" {
		t.Fatalf("next = %+v, %v; want senior-1", w, ok)
	}
}
