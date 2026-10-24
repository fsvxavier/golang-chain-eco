package team

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// This file reads what Claude Code itself keeps on disk: the sub-agents defined
// for a project (.claude/agents/*.md), the runs of those sub-agents from the
// chat, and the conversations (jsonl sessions) of the CLI calls. Everything is
// read-only and local.

// ChatAgent is a sub-agent defined in .claude/agents, with its runs from the chat.
type ChatAgent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Model       string `json:"model,omitempty"`
	Tools       string `json:"tools,omitempty"`
	Source      string `json:"source"` // project or user
	Runs        int    `json:"runs"`
	Active      int    `json:"active"` // runs still writing
}

// ChatRun is one run of a sub-agent in the chat.
type ChatRun struct {
	ID          string `json:"id"` // agentId, readable with ChatTranscript
	Agent       string `json:"agent"`
	Description string `json:"description,omitempty"`
	StartedMS   int64  `json:"started_ms"`
	UpdatedMS   int64  `json:"updated_ms"`
	Running     bool   `json:"running"`
}

// Turn is one message of a transcript.
type Turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
	MS   int64  `json:"ms,omitempty"`
}

const (
	maxTurns   = 300
	maxTurnLen = 3000
	runningFor = 90 * time.Second // a run that wrote within this is shown as running
	maxRuns    = 60
)

var (
	uuidRe  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	agentRe = regexp.MustCompile(`^[a-zA-Z0-9]{1,64}$`)
	nonAlnu = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}

// projectDirs are the folders of ~/.claude/projects that belong to root (its
// path with every non-alphanumeric character turned into "-", also resolved).
func projectDirs(root string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range []string{root, resolvedOr(root)} {
		d := filepath.Join(claudeDir(), "projects", nonAlnu.ReplaceAllString(r, "-"))
		if r != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

func resolvedOr(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// frontmatter returns the key: value lines between the leading --- lines.
func frontmatter(b []byte) map[string]string {
	m := map[string]string{}
	lines := strings.Split(string(b), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return m
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			break
		}
		if k, v, ok := strings.Cut(l, ":"); ok && !strings.HasPrefix(l, " ") {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func firstLine(s string, n int) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}

type chatCache struct {
	mu     sync.Mutex
	at     time.Time
	root   string
	agents []ChatAgent
	runs   []ChatRun
}

var chatCached chatCache

// ChatAgents lists the sub-agents of root's .claude/agents (and the user's) and
// the recent runs of them in root's chat sessions. It is cached for 5 seconds,
// because the dashboard polls.
func ChatAgents(root string) ([]ChatAgent, []ChatRun) {
	c := &chatCached
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == root && time.Since(c.at) < 5*time.Second {
		return c.agents, c.runs
	}
	agents := []ChatAgent{}
	byName := map[string]int{}
	for _, src := range []struct{ dir, name string }{{filepath.Join(root, ".claude", "agents"), "project"}, {filepath.Join(claudeDir(), "agents"), "user"}} {
		es, _ := os.ReadDir(src.dir)
		for _, e := range es {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(src.dir, e.Name()))
			if err != nil {
				continue
			}
			fm := frontmatter(b)
			name := strings.Trim(fm["name"], `"'`)
			if name == "" {
				name = strings.TrimSuffix(e.Name(), ".md")
			}
			if _, dup := byName[name]; dup {
				continue // the project's definition wins
			}
			byName[name] = len(agents)
			agents = append(agents, ChatAgent{Name: name, Description: firstLine(fm["description"], 160), Model: fm["model"], Tools: fm["tools"], Source: src.name})
		}
	}
	runs := []ChatRun{}
	for _, pd := range projectDirs(root) {
		metas, _ := filepath.Glob(filepath.Join(pd, "*", "subagents", "agent-*.meta.json"))
		for _, mp := range metas {
			var meta struct{ AgentType, Description string }
			if b, err := os.ReadFile(mp); err != nil || json.Unmarshal(b, &meta) != nil {
				continue
			}
			jp := strings.TrimSuffix(mp, ".meta.json") + ".jsonl"
			fi, err := os.Stat(jp)
			if err != nil {
				continue
			}
			id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(jp), "agent-"), ".jsonl")
			runs = append(runs, ChatRun{ID: id, Agent: meta.AgentType, Description: firstLine(meta.Description, 120),
				StartedMS: firstTimestamp(jp, fi.ModTime().UnixMilli()), UpdatedMS: fi.ModTime().UnixMilli(),
				Running: time.Since(fi.ModTime()) < runningFor})
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].UpdatedMS > runs[j].UpdatedMS })
	if len(runs) > maxRuns {
		runs = runs[:maxRuns]
	}
	for _, r := range runs {
		i, ok := byName[r.Agent]
		if !ok { // a built-in sub-agent type (Explore, general-purpose): listed once it has run
			i = len(agents)
			byName[r.Agent] = i
			agents = append(agents, ChatAgent{Name: r.Agent, Source: "built-in"})
		}
		agents[i].Runs++
		if r.Running {
			agents[i].Active++
		}
	}
	c.at, c.root, c.agents, c.runs = time.Now(), root, agents, runs
	return agents, runs
}

func firstTimestamp(path string, fallback int64) int64 {
	f, err := os.Open(path)
	if err != nil {
		return fallback
	}
	defer f.Close()
	line, _ := bufio.NewReaderSize(f, 1<<20).ReadBytes('\n')
	var l struct{ Timestamp time.Time }
	if json.Unmarshal(line, &l) != nil || l.Timestamp.IsZero() {
		return fallback
	}
	return l.Timestamp.UnixMilli()
}

// CLITranscriptPath finds the session file of a CLI call, wherever Claude Code
// filed it.
func CLITranscriptPath(session string) (string, error) {
	if !uuidRe.MatchString(session) {
		return "", fmt.Errorf("not a session id")
	}
	m, _ := filepath.Glob(filepath.Join(claudeDir(), "projects", "*", session+".jsonl"))
	if len(m) == 0 {
		return "", fmt.Errorf("no transcript for that session (CLAUDE_CLI_TRANSCRIPTS=off, or not written yet)")
	}
	return m[0], nil
}

// ChatTranscriptPath finds the file of a sub-agent run of root's chat.
func ChatTranscriptPath(root, id string) (string, error) {
	if !agentRe.MatchString(id) {
		return "", fmt.Errorf("not an agent id")
	}
	for _, pd := range projectDirs(root) {
		if m, _ := filepath.Glob(filepath.Join(pd, "*", "subagents", "agent-"+id+".jsonl")); len(m) > 0 {
			return m[0], nil
		}
	}
	return "", fmt.Errorf("no such run")
}

// ReadTranscript turns a Claude Code jsonl session into turns: the text of the
// user and assistant messages, tool calls and results in one line each. The
// newest maxTurns are kept and each is cut at maxTurnLen.
func ReadTranscript(path string) ([]Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 32<<20)
	turns := []Turn{}
	for sc.Scan() {
		var l struct {
			Type      string    `json:"type"`
			IsMeta    bool      `json:"isMeta"`
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.IsMeta || (l.Type != "user" && l.Type != "assistant") {
			continue
		}
		for _, t := range contentTurns(l.Message.Role, l.Message.Content) {
			t.MS = l.Timestamp.UnixMilli()
			turns = append(turns, t)
		}
	}
	if err := sc.Err(); err != nil {
		return turns, err
	}
	if len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
	}
	return turns, nil
}

func cut(s string) string {
	if r := []rune(s); len(r) > maxTurnLen {
		return string(r[:maxTurnLen]) + "…"
	}
	return s
}

func contentTurns(role string, raw json.RawMessage) []Turn {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if strings.HasPrefix(strings.TrimSpace(s), "<system-reminder>") {
			return nil
		}
		return []Turn{{Role: role, Text: cut(s)}}
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
		Input   json.RawMessage `json:"input"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	var out []Turn
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" && !strings.HasPrefix(t, "<system-reminder>") {
				out = append(out, Turn{Role: role, Text: cut(b.Text)})
			}
		case "tool_use":
			out = append(out, Turn{Role: "tool", Text: cut("call " + b.Name + " " + string(b.Input))})
		case "tool_result":
			var rs string
			if json.Unmarshal(b.Content, &rs) != nil {
				rs = string(b.Content)
			}
			if r := []rune(rs); len(r) > 600 {
				rs = string(r[:600]) + "…"
			}
			out = append(out, Turn{Role: "tool", Text: "result " + rs})
		}
	}
	return out
}
