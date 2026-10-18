package channels_test

import (
	"testing"

	"github.com/fsvxavier/golang-chain-eco/langgraph/channels"
)

func TestChannels(t *testing.T) {
	lv := channels.LastValue()()
	if _, err := lv.Update([]any{1, 2}); err == nil {
		t.Fatal("two writes to LastValue must fail")
	}
	if _, err := lv.Update([]any{3}); err != nil {
		t.Fatal(err)
	}
	if v, ok := lv.Get(); !ok || v != 3 {
		t.Fatal(v)
	}
	sum := channels.BinOp(func(a, b any) (any, error) { return a.(int) + b.(int), nil }, func() any { return 0 })()
	_, _ = sum.Update([]any{1, 2, 3})
	if v, _ := sum.Get(); v != 6 {
		t.Fatal(v)
	}
	cp, ok := sum.Checkpoint()
	if v, _ := sum.Restore(cp, ok).Get(); v != 6 {
		t.Fatal("restore")
	}
	b := channels.NamedBarrier("a", "b")()
	_, _ = b.Update([]any{"a"})
	if _, ok := b.Get(); ok {
		t.Fatal("barrier early")
	}
	_, _ = b.Update([]any{"b"})
	if _, ok := b.Get(); !ok || !b.Consume() {
		t.Fatal("barrier")
	}
	if _, ok := b.Get(); ok {
		t.Fatal("barrier must reset")
	}
}
