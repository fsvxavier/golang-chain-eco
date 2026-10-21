// Package router is a chat model that fans requests out over several
// providers, failing over to the next target on retryable errors.
package router

import (
	"context"
	"errors"

	"github.com/fsvxavier/golang-chain-eco/providers/internal/httpx"
)

// retryable reports whether a failed call to one target is worth retrying
// on the next one. Network errors, per-target timeouts, HTTP 408, 425, 429
// and every 5xx are retryable; any other HTTP status is final. Once the
// caller's context is done nothing is retryable.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var se *httpx.StatusError
	if errors.As(err, &se) {
		switch {
		case se.Code == 408, se.Code == 425, se.Code == 429:
			return true
		case se.Code >= 500 && se.Code <= 599:
			return true
		default:
			return false
		}
	}
	return true
}
