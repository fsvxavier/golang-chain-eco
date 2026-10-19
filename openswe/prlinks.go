package openswe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
	"github.com/fsvxavier/golang-chain-eco/openswe/github"
)

// PRLink ties a pull request to the thread that delivered it.
type PRLink struct {
	URL string `json:"url"`
	// ResolvesThread says the thread is finished once the PR merges or closes
	// (false while more PRs are planned, e.g. a stack).
	ResolvesThread bool `json:"resolves_thread"`
}

// PRLinks stores thread <-> pull request links, so a comment on a PR finds the
// coding thread that made it, and a thread knows which PRs it owns.
type PRLinks struct {
	S  store.Store
	mu sync.Mutex
}

func linkNS(kind string) []string { return []string{"openswe", "prlinks", kind} }

func prKeyString(k github.PRKey) string {
	return strings.ToLower(fmt.Sprintf("%s/%s#%d", k.Owner, k.Repo, k.Number))
}

// Link records that thread owns pr.
func (l *PRLinks) Link(ctx context.Context, thread string, pr github.PRKey, resolves bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	links, err := l.byThread(ctx, thread)
	if err != nil {
		return err
	}
	found := false
	for i := range links {
		if links[i].URL == pr.String() {
			links[i].ResolvesThread, found = resolves, true
		}
	}
	if !found {
		links = append(links, PRLink{URL: pr.String(), ResolvesThread: resolves})
	}
	b, _ := json.Marshal(links)
	var raw []any
	_ = json.Unmarshal(b, &raw)
	if err := l.S.Put(ctx, linkNS("thread"), thread, map[string]any{"links": raw}); err != nil {
		return err
	}
	return l.S.Put(ctx, linkNS("pr"), prKeyString(pr), map[string]any{"thread": thread})
}

func (l *PRLinks) byThread(ctx context.Context, thread string) ([]PRLink, error) {
	it, err := l.S.Get(ctx, linkNS("thread"), thread)
	if err != nil || it == nil {
		return nil, err
	}
	b, _ := json.Marshal(it.Value["links"])
	var links []PRLink
	return links, json.Unmarshal(b, &links)
}

// ForThread lists a thread's PRs.
func (l *PRLinks) ForThread(ctx context.Context, thread string) ([]PRLink, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.byThread(ctx, thread)
}

// ThreadFor returns the thread that owns a PR ("" when none).
func (l *PRLinks) ThreadFor(ctx context.Context, pr github.PRKey) (string, error) {
	it, err := l.S.Get(ctx, linkNS("pr"), prKeyString(pr))
	if err != nil || it == nil {
		return "", err
	}
	t, _ := it.Value["thread"].(string)
	return t, nil
}
