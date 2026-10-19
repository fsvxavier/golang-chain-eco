package langmem_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/langmem"
)

// patchCall is a PatchDoc tool call on memory id with the given RFC 6902 ops.
func patchCall(id string, ops ...map[string]any) messages.ToolCall {
	patches := make([]any, len(ops))
	for i, op := range ops {
		patches[i] = op
	}
	return tc("PatchDoc", map[string]any{"json_doc_id": id, "planned_edits": "edit", "patches": patches})
}

// seen reports whether any message sent to the model in call n contains s.
func seen(m *models.Fake, n int, s string) bool {
	if n >= len(m.Calls) {
		return false
	}
	for _, msg := range m.Calls[n] {
		if strings.Contains(msg.Text(), s) {
			return true
		}
	}
	return false
}

func TestPatchDocApplied(t *testing.T) {
	existing := []langmem.Extracted{langmem.Note("e1", "old")}
	m := script(messages.AI("",
		patchCall("e1", map[string]any{"op": "replace", "path": "/content", "value": "new"}),
		tc("Done", nil)))
	out, err := langmem.NewManager(m, langmem.WithPatchUpdates()).Extract(context.Background(), conv("x"), existing, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "e1" || langmem.Text(out[0].Content) != "new" {
		t.Fatalf("%+v", out)
	}
	if len(m.Calls) != 1 {
		t.Fatalf("a valid patch needs one round, got %d", len(m.Calls))
	}
}

// A failed patch is sent back to the model and gets one retry even with
// maxSteps=1; the retry succeeds.
func TestPatchDocRetryThenSuccess(t *testing.T) {
	existing := []langmem.Extracted{langmem.Note("e1", "old")}
	m := script(
		messages.AI("", patchCall("e1", map[string]any{"op": "test", "path": "/content", "value": "WRONG"})),
		messages.AI("", patchCall("e1", map[string]any{"op": "replace", "path": "/content", "value": "fixed"}), tc("Done", nil)))
	out, err := langmem.NewManager(m, langmem.WithPatchUpdates()).Extract(context.Background(), conv("x"), existing, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || langmem.Text(out[0].Content) != "fixed" {
		t.Fatalf("%+v", out)
	}
	if len(m.Calls) != 2 || !seen(m, 1, "one retry") {
		t.Fatalf("the error must be returned to the model for a retry: calls=%d", len(m.Calls))
	}
}

// Two failed patches fall back to replacement: the model is told to resend
// the complete content with json_doc_id, and that content is stored.
func TestPatchDocFallbackAfterTwoFailures(t *testing.T) {
	existing := []langmem.Extracted{langmem.Note("e1", "old")}
	bad := map[string]any{"op": "test", "path": "/content", "value": "WRONG"}
	m := script(
		messages.AI("", patchCall("e1", bad)),
		messages.AI("", patchCall("e1", bad)),
		messages.AI("", tc("Memory", map[string]any{"content": "full replacement", "json_doc_id": "e1"}), tc("Done", nil)))
	out, err := langmem.NewManager(m, langmem.WithPatchUpdates()).Extract(context.Background(), conv("x"), existing, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "e1" || langmem.Text(out[0].Content) != "full replacement" {
		t.Fatalf("%+v", out)
	}
	if len(m.Calls) != 3 || !seen(m, 2, "failed twice") {
		t.Fatalf("the second failure must point at the replacement path: calls=%d", len(m.Calls))
	}
}

// Without the option PatchDoc is not offered and calling it is an error; the
// replacement path is untouched.
func TestPatchDocOffOnlyWithOption(t *testing.T) {
	existing := []langmem.Extracted{langmem.Note("e1", "old")}
	m := script(messages.AI("", patchCall("e1", map[string]any{"op": "replace", "path": "/content", "value": "new"})))
	out, _ := langmem.NewManager(m).Extract(context.Background(), conv("x"), existing, 1)
	if len(out) != 1 || langmem.Text(out[0].Content) != "old" {
		t.Fatalf("PatchDoc must be ignored by default: %+v", out)
	}
}
