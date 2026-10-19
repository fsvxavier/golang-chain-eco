package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestStdioHostWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	const missing = "/nonexistent/mcp-server-for-test"
	ctx := context.Background()
	if _, err := stdio(ctx, missing, nil, nil, true); err == nil {
		t.Fatal("the command does not exist")
	}
	if buf.Len() != 0 {
		t.Fatalf("Quiet must not warn: %s", buf.String())
	}
	_, _ = stdio(ctx, missing, nil, nil, false)
	_, _ = stdio(ctx, missing, nil, nil, false)
	if n := strings.Count(buf.String(), "stdio servers are started on the host"); n != 1 {
		t.Fatalf("want exactly one warning, got %d: %s", n, buf.String())
	}
}
