//go:build !laya_native

package main

import (
	"context"
	"errors"

	"github.com/fsvxavier/golang-chain-eco/compress"
)

// openDecider has no native backend in this build, so main falls back to the rules.
func openDecider(_ context.Context, _ string) (compress.Decider, func(), error) {
	return nil, nil, errors.New("built without -tags laya_native")
}
