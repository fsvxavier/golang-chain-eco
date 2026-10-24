package team

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/remote"
)

// Cancelling while the team starts must return the ctx error at once, not
// poll for 10 s and report "did not come up".
func TestStartLocalWithHonoursCancel(t *testing.T) {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	l, err := StartLocalWith(c, LocalOptions{})
	if !errors.Is(err, context.Canceled) || l != nil {
		t.Fatalf("l=%v err=%v, want nil and context.Canceled", l, err)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("returned after %s", el)
	}
}

// With ManagerAPIKey only a client holding it reaches the manager.
func TestManagerAPIKeyProtectsManager(t *testing.T) {
	c := ctx(t)
	l, err := StartLocalWith(c, LocalOptions{ManagerAPIKey: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(l.ManagerURL+"/threads", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("keyless POST = %d, want 401/403", resp.StatusCode)
	}
	good := remote.NewClient(l.ManagerURL)
	good.APIKey = "s3cret"
	if _, err := good.CreateThread(c, map[string]any{}); err != nil {
		t.Fatalf("keyed client: %v", err)
	}
}
