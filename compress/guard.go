package compress

import (
	"context"
	"errors"
	"time"
)

// Guarded asks Inner first and falls back to Fallback question by question.
// An Inner answer is used only when it exists, matches the question's id and
// kind, and is Confident(Floor). Inner errors, timeouts and panics send every
// question to Fallback. Cancellation of the caller's ctx is returned as an error.
type Guarded struct {
	Inner    Decider
	Fallback Decider
	Floor    float64
	Timeout  time.Duration
}

// NewGuarded wraps inner with the defaults: Fallback RuleDecider{}, Floor 0.7,
// Timeout 2s.
func NewGuarded(inner Decider) Guarded {
	return Guarded{Inner: inner, Fallback: RuleDecider{}, Floor: 0.7, Timeout: 2 * time.Second}
}

// Decide implements Decider.
func (g Guarded) Decide(ctx context.Context, st State, qs []Question) ([]Answer, error) {
	fb := g.Fallback
	if fb == nil {
		fb = RuleDecider{}
	}
	got := g.askInner(ctx, st, qs)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	byID := make(map[string]Answer, len(got))
	for _, a := range got {
		if _, dup := byID[a.ID]; !dup {
			byID[a.ID] = a
		}
	}

	out := make([]Answer, len(qs))
	var need []Question
	var idx []int
	for i, q := range qs {
		if a, ok := byID[q.ID]; ok && g.usable(a, q) {
			out[i] = a
			continue
		}
		need = append(need, q)
		idx = append(idx, i)
	}
	if len(need) == 0 {
		return out, nil
	}

	fa, err := fb.Decide(ctx, st, need)
	if err != nil {
		return nil, err
	}
	if len(fa) != len(need) {
		return nil, errGuardShape
	}
	for j, i := range idx {
		out[i] = fa[j]
	}
	return out, nil
}

// usable reports whether an inner answer may replace the fallback for q.
func (g Guarded) usable(a Answer, q Question) bool {
	if a.ID != q.ID || a.Kind != q.Kind || !a.Confident(g.Floor) {
		return false
	}
	if q.Kind == KindChoice && len(q.Options) > 0 && !ruleContains(q.Options, a.Pick) {
		return false
	}
	return true
}

// askInner runs Inner under the timeout. It returns nil on error, timeout or
// panic, so the caller falls back.
func (g Guarded) askInner(ctx context.Context, st State, qs []Question) []Answer {
	if g.Inner == nil {
		return nil
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ictx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		answers []Answer
		err     error
	}
	// Buffered, so a decider that ignores the timeout does not block forever.
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{err: errGuardPanic}
			}
		}()
		a, err := g.Inner.Decide(ictx, st, qs)
		ch <- result{answers: a, err: err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return nil
		}
		return r.answers
	case <-ictx.Done():
		return nil
	}
}

var (
	errGuardPanic = errors.New("compress: decider panicked")
	errGuardShape = errors.New("compress: fallback returned wrong number of answers")
)
