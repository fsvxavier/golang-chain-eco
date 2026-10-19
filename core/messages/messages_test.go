package messages_test

import (
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
)

func TestUsageAddCache(t *testing.T) {
	a := &messages.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CacheReadTokens: 4, CacheCreationTokens: 2}
	b := &messages.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, CacheReadTokens: 1}
	s := a.Add(b)
	if s.CacheReadTokens != 5 || s.CacheCreationTokens != 2 || s.InputTokens != 11 || s.TotalTokens != 17 {
		t.Fatalf("%+v", s)
	}
}
