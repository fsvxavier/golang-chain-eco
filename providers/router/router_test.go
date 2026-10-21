package router

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

func TestRetryable(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"408", &httpx.StatusError{Code: 408}, true},
		{"425", &httpx.StatusError{Code: 425}, true},
		{"429", &httpx.StatusError{Code: 429}, true},
		{"500", &httpx.StatusError{Code: 500}, true},
		{"502", &httpx.StatusError{Code: 502}, true},
		{"503", &httpx.StatusError{Code: 503}, true},
		{"529", &httpx.StatusError{Code: 529}, true},
		{"400", &httpx.StatusError{Code: 400}, false},
		{"401", &httpx.StatusError{Code: 401}, false},
		{"403", &httpx.StatusError{Code: 403}, false},
		{"404", &httpx.StatusError{Code: 404}, false},
		{"422", &httpx.StatusError{Code: 422}, false},
		{"wrapped 503", fmt.Errorf("x: %w", &httpx.StatusError{Code: 503}), true},
		{"wrapped 400", fmt.Errorf("x: %w", &httpx.StatusError{Code: 400}), false},
		{"network error", errors.New("connection reset"), true},
		{"attempt deadline", context.DeadlineExceeded, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryable(live, tc.err); got != tc.want {
				t.Errorf("retryable(live, %v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}

	// Once the caller's context is done, nothing is retryable.
	for _, tc := range cases {
		t.Run("cancelled/"+tc.name, func(t *testing.T) {
			if got := retryable(cancelled, tc.err); got {
				t.Errorf("retryable(cancelled, %v) = true, want false", tc.err)
			}
		})
	}
}
