// content_builder: a deep agent that follows a brand-voice memory file and
// skills to write a blog post, saving files into a project directory
// (port of deepagents/examples/content-builder-agent).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/deepagents"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

func call(id, name string, args map[string]any) messages.ToolCall {
	return messages.ToolCall{ID: id, Name: name, Args: args}
}

func main() {
	dir, err := os.MkdirTemp("", "content-builder-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mustWrite := func(rel, content string) {
		p := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			log.Fatal(err)
		}
	}
	mustWrite("AGENTS.md", "# Brand voice\n- Friendly, concrete, no buzzwords.\n- Every post ends with a call to action.\n")
	mustWrite("skills/blog-post/SKILL.md", "---\nname: blog-post\ndescription: Structure and checklist for blog posts\n---\n1. Hook\n2. Three key points\n3. Call to action\n")

	lead := models.NewFake(
		messages.AI("", call("1", "read_file", map[string]any{"file_path": "/skills/blog-post/SKILL.md"})),
		messages.AI("", call("2", "write_file", map[string]any{"file_path": "/blog/go-concurrency.md",
			"content": "# Why Go makes concurrency approachable\n\nHook...\n\n- goroutines\n- channels\n- select\n\nTry the tour at go.dev/tour today!\n"})),
		messages.AI("Draft saved to /blog/go-concurrency.md following the blog-post skill and brand voice."),
	)
	agent, err := deepagents.Create(deepagents.Options{
		Model: lead, SummarizeAt: -1,
		Backend: backends.Filesystem{Root: dir}, // real files, confined to dir
		Memory:  []string{"/AGENTS.md"},
		Skills:  []string{"/skills/"},
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := agent.Invoke(context.Background(), graph.State{"messages": "Write a blog post about Go concurrency."})
	if err != nil {
		log.Fatal(err)
	}
	ms := out["messages"].([]messages.Message)
	fmt.Println(ms[len(ms)-1].Text())
	b, _ := os.ReadFile(filepath.Join(dir, "blog", "go-concurrency.md"))
	fmt.Printf("--- on disk: blog/go-concurrency.md ---\n%s", b)
	// the system prompt the model saw included the memory and the skill index
	for _, m := range lead.Calls[0] {
		if m.Role == messages.RoleSystem {
			fmt.Println("--- system prompt mentions memory:", contains(m.Text(), "Brand voice"), "| skill:", contains(m.Text(), "blog-post"))
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
