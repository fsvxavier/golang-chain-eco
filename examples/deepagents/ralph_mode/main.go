// ralph_mode: run the same agent in a loop with a fresh context each time,
// carrying progress only through files, until the task is done
// (idea of deepagents/examples/ralph_mode).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func main() {
	dir, err := os.MkdirTemp("", "ralph-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	be := backends.Filesystem{Root: dir}
	ctx := context.Background()

	// The "model": each iteration reads progress.md, does ONE item, appends to it.
	items := []string{"design API", "implement handlers", "write tests"}
	model := &models.Fake{Func: func(msgs []messages.Message, _ models.Options) (messages.Message, error) {
		last := msgs[len(msgs)-1]
		if last.Role == messages.RoleTool && strings.HasPrefix(last.Name, "read_file") {
			done := strings.Count(last.Text(), "[x]")
			if done >= len(items) {
				return messages.AI("ALL DONE"), nil
			}
			return messages.AI("", messages.ToolCall{Name: "edit_file", Args: map[string]any{
				"file_path": "/progress.md", "old_string": "[ ] " + items[done], "new_string": "[x] " + items[done], "replace_all": false}}), nil
		}
		if last.Role == messages.RoleTool {
			return messages.AI("Finished one item."), nil
		}
		return messages.AI("", messages.ToolCall{Name: "read_file", Args: map[string]any{"file_path": "/progress.md"}}), nil
	}}
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString("[ ] " + it + "\n")
	}
	if _, err := be.Write(ctx, "/progress.md", sb.String()); err != nil {
		log.Fatal(err)
	}
	for i := 1; i <= 6; i++ {
		agent, err := deepagents.Create(deepagents.Options{Model: model, Backend: be, SummarizeAt: -1})
		if err != nil {
			log.Fatal(err)
		}
		// fresh agent, fresh conversation: only the files carry state
		out, err := agent.Invoke(ctx, graph.State{"messages": "Read /progress.md and complete exactly one unchecked item."})
		if err != nil {
			log.Fatal(err)
		}
		progress, _ := be.Read(ctx, "/progress.md")
		fmt.Printf("iteration %d: %s | checked=%d\n", i, strings.TrimSpace(lastText(out)), strings.Count(progress, "[x]"))
		if strings.Contains(lastText(out), "ALL DONE") {
			break
		}
	}
}

func lastText(out graph.State) string {
	ms, _ := messages.Coerce(out["messages"])
	return ms[len(ms)-1].Text()
}
