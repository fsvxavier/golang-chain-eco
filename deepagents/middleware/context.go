package middleware

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/agents"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
)

// PatchToolCalls repairs history where an AI tool call has no matching tool
// result (e.g. the user interrupted): it inserts a cancellation tool message
// so providers do not reject the conversation (PatchToolCallsMiddleware).
type PatchToolCalls struct{}

func (PatchToolCalls) Name() string { return "patch_tool_calls" }

func (PatchToolCalls) BeforeAgent(_ context.Context, st graph.State) (any, error) {
	msgs, _ := messages.Coerce(st["messages"])
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == messages.RoleTool {
			answered[m.ToolCallID] = true
		}
	}
	var out []messages.Message
	patched := false
	for _, m := range msgs {
		out = append(out, m)
		if m.Role != messages.RoleAI {
			continue
		}
		for _, c := range m.ToolCalls {
			if !answered[c.ID] {
				t := messages.Tool(fmt.Sprintf("Tool call %s with id %s was cancelled - another message came in before it could be completed.", c.Name, c.ID), c.ID)
				t.Name = c.Name
				out = append(out, t)
				patched = true
			}
		}
	}
	if !patched {
		return nil, nil
	}
	return graph.State{"messages": append([]messages.Message{messages.Remove(messages.RemoveAll)}, out...)}, nil
}

// Memory injects the content of memory files (e.g. AGENTS.md) into the
// system prompt (MemoryMiddleware).
type Memory struct {
	Backend backends.Backend
	Sources []string
}

func (Memory) Name() string { return "memory" }

func (m Memory) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	var b strings.Builder
	for _, src := range m.Sources {
		c, err := m.Backend.Read(ctx, src)
		if err != nil {
			continue // missing memory files are normal
		}
		fmt.Fprintf(&b, "%s\n%s\n\n", src, strings.TrimSpace(c))
	}
	if b.Len() == 0 {
		return next(ctx, req)
	}
	r := *req
	r.System = strings.TrimSpace(req.System + "\n\n<agent_memory>\n" + strings.TrimSpace(b.String()) +
		"\n</agent_memory>\n\nThe above is your persistent memory. Update it with edit_file when you learn durable preferences or facts.")
	return next(ctx, &r)
}

// Skill is a parsed SKILL.md header.
type Skill struct {
	Name, Description, Path string
}

// Skills advertises skills found under Sources (directories containing
// <skill>/SKILL.md with name/description front matter). The agent reads the
// full SKILL.md on demand (progressive disclosure).
type Skills struct {
	Backend backends.Backend
	Sources []string
}

func (Skills) Name() string { return "skills" }

// Discover lists the skills available to the backend.
func (s Skills) Discover(ctx context.Context) []Skill {
	var out []Skill
	for _, src := range s.Sources {
		infos, err := s.Backend.Ls(ctx, src)
		if err != nil {
			continue
		}
		for _, fi := range infos {
			if !fi.IsDir {
				continue
			}
			p := path.Join(fi.Path, "SKILL.md")
			c, err := s.Backend.Read(ctx, p)
			if err != nil {
				continue
			}
			name, desc := frontMatter(c)
			if name == "" {
				name = path.Base(strings.TrimSuffix(fi.Path, "/"))
			}
			out = append(out, Skill{Name: name, Description: desc, Path: p})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func frontMatter(c string) (name, desc string) {
	if !strings.HasPrefix(c, "---") {
		return
	}
	rest := strings.TrimPrefix(c, "---")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "description":
			desc = v
		}
	}
	return
}

func (s Skills) WrapModelCall(ctx context.Context, req *agents.ModelRequest, next agents.ModelCaller) (messages.Message, error) {
	sk := s.Discover(ctx)
	if len(sk) == 0 {
		return next(ctx, req)
	}
	var b strings.Builder
	b.WriteString("## Skills\nWhen a task matches a skill, read its SKILL.md with read_file before acting.\n")
	for _, k := range sk {
		fmt.Fprintf(&b, "- %s: %s (read %s)\n", k.Name, k.Description, k.Path)
	}
	r := *req
	r.System = strings.TrimSpace(req.System + "\n\n" + b.String())
	return next(ctx, &r)
}
