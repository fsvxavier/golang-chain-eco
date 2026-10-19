package langmem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/internal/jsonpatch"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// DefaultInstructions is LangMem's memory-manager prompt (semantic,
// procedural and episodic memory for a life-long learning agent).
const DefaultInstructions = `You are a long-term memory manager maintaining a core store of semantic, procedural, and episodic memory. These memories power a life-long learning agent's core predictive model.

What should the agent learn from this interaction about the user, itself, or how it should act? Reflect on the input trajectory and current memories (if any).

1. **Extract & Contextualize**
   - Identify essential facts, relationships, preferences, reasoning procedures, and context
   - Caveat uncertain or suppositional information with confidence levels (p(x)) and reasoning
   - Quote supporting information when necessary

2. **Compare & Update**
   - Attend to novel information that deviates from existing memories and expectations.
   - Consolidate and compress redundant memories to maintain information-density; strengthen based on reliability and recency; maximize SNR by avoiding idle words.
   - Remove incorrect or redundant memories while maintaining internal consistency

3. **Synthesize & Reason**
   - What can you conclude about the user, agent ("I"), or environment using deduction, induction, and abduction?
   - What patterns, relationships, and principles emerge about optimal responses?
   - What generalizations can you make?
   - Qualify conclusions with probabilistic confidence and justification

As the agent, record memory content exactly as you'd want to recall it when predicting how to act or respond.
Prioritize retention of surprising (pattern deviation) and persistent (frequently reinforced) information, ensuring nothing worth remembering is forgotten and nothing false is remembered. Prefer dense, complete memories over overlapping ones.`

const (
	removeTool = "RemoveDoc"
	doneTool   = "Done"
	patchTool  = "PatchDoc"
	idArg      = "json_doc_id"
)

type config struct {
	schemas      []Schema
	instructions string
	inserts      bool
	updates      bool
	deletes      bool
	patchUpdates bool

	// store manager only
	store          store.Store
	queryModel     models.ChatModel
	queryLimit     int
	defaultSeed    *Seed
	defaultFactory func(context.Context) (Seed, error)
	phases         []Phase
}

// Option customises NewManager and NewStoreManager.
type Option func(*config)

// WithSchemas sets the memory kinds the model may write (default: free-text
// MemorySchema).
func WithSchemas(s ...Schema) Option { return func(c *config) { c.schemas = s } }

// WithInstructions replaces DefaultInstructions.
func WithInstructions(s string) Option { return func(c *config) { c.instructions = s } }

// WithInserts allows (default) or forbids creating new memories. Disable it
// for a profile: one document that is only ever updated.
func WithInserts(b bool) Option { return func(c *config) { c.inserts = b } }

// WithUpdates allows (default) or forbids changing existing memories.
func WithUpdates(b bool) Option { return func(c *config) { c.updates = b } }

// WithPatchUpdates makes updates of existing memories go through PatchDoc:
// RFC 6902 operations on the stored content instead of resending the whole
// memory (trustcall's PatchDoc). A failed patch is returned to the model for
// one retry; a second failure falls back to replacing the memory with its
// complete content. Replacement stays the default.
func WithPatchUpdates() Option { return func(c *config) { c.patchUpdates = true } }

// WithDeletes allows or forbids removing memories. Default: false for a
// Manager, true for a StoreManager (as in LangMem).
func WithDeletes(b bool) Option { return func(c *config) { c.deletes = b } }

func newConfig(deletes bool, opts []Option) config {
	c := config{inserts: true, updates: true, deletes: deletes, instructions: DefaultInstructions}
	for _, o := range opts {
		o(&c)
	}
	if len(c.schemas) == 0 {
		c.schemas = []Schema{MemorySchema}
	}
	return c
}

// Manager extracts, updates and consolidates memories from conversations with
// a chat model (create_memory_manager). It is pure: it reads no store. Pass
// the existing memories in and get the resulting list out; StoreManager
// persists it.
//
// Each schema is a tool; the model records memories with parallel tool calls,
// updates one by repeating its json_doc_id, and deletes with RemoveDoc.
type Manager struct {
	Model models.ChatModel
	cfg   config
}

// NewManager builds a manager. Defaults: inserts and updates on, deletes off.
func NewManager(model models.ChatModel, opts ...Option) *Manager {
	return &Manager{Model: model, cfg: newConfig(false, opts)}
}

func (m *Manager) schemaByName(name string) (Schema, bool) {
	for _, s := range m.cfg.schemas {
		if s.Name == name {
			return s, true
		}
	}
	return Schema{}, false
}

func (m *Manager) toolDefs(live []Extracted, withDone bool) []tools.Definition {
	var defs []tools.Definition
	for _, s := range m.cfg.schemas {
		p := s.Parameters.Map()
		props, _ := p["properties"].(map[string]any)
		if props == nil {
			props = map[string]any{}
			p["properties"] = props
		}
		desc := s.Description
		if m.cfg.updates && len(live) > 0 {
			props[idArg] = map[string]any{"type": "string", "description": "ID of an existing memory to replace with this complete content. Omit to create a new memory."}
			if m.cfg.patchUpdates {
				desc += " To update an existing memory, use " + patchTool + ". Set " + idArg + " and provide the complete new content only if " + patchTool + " failed twice."
			} else {
				desc += " To update an existing memory, set " + idArg + " and provide its complete new content."
			}
		}
		delete(p, "title")
		defs = append(defs, tools.Definition{Name: s.Name, Description: desc, Parameters: p})
	}
	if m.cfg.patchUpdates && m.cfg.updates && len(live) > 0 {
		defs = append(defs, tools.Definition{
			Name: patchTool,
			Description: "Update an existing memory with RFC 6902 JSON Patch operations on its content. " +
				"Paths are JSON Pointers into the content, e.g. \"/content\" for the default schema. " +
				"planned_edits briefly states what changes. Each patch is {op, path, value|from}; ops: add, remove, replace, move, copy, test.",
			Parameters: map[string]any{"type": "object", "required": []string{idArg, "planned_edits", "patches"},
				"properties": map[string]any{
					idArg:           map[string]any{"type": "string", "description": "ID of the memory to patch"},
					"planned_edits": map[string]any{"type": "string", "description": "What you plan to change, in one or two sentences"},
					"patches": map[string]any{"type": "array", "description": "JSON Patch operations (RFC 6902)",
						"items": map[string]any{"type": "object", "required": []string{"op", "path"}}},
				}},
		})
	}
	if m.cfg.deletes && len(live) > 0 {
		defs = append(defs, tools.Definition{
			Name:        removeTool,
			Description: "Delete an existing memory that is wrong, obsolete or redundant.",
			Parameters: map[string]any{"type": "object", "required": []string{idArg},
				"properties": map[string]any{idArg: map[string]any{"type": "string", "description": "ID of the memory to delete"}}},
		})
	}
	if withDone {
		defs = append(defs, tools.Definition{
			Name:        doneTool,
			Description: "Only call this tool once you are done forming & consolidating memories. Before that, continue to refine existing memories by patching and removing them or create new ones.",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}
	return defs
}

func renderExisting(live []Extracted) string {
	var b strings.Builder
	b.WriteString("<existing_memories>\n")
	for _, e := range live {
		c, _ := json.Marshal(e.Content)
		fmt.Fprintf(&b, "<memory id=%q kind=%q>%s</memory>\n", e.ID, e.Kind, c)
	}
	b.WriteString("</existing_memories>")
	return b.String()
}

// Extract returns the full memory list after the conversation: existing
// memories (changed or not) plus new ones; existing ones the model removed
// come back with Deleted set. maxSteps > 1 lets the model refine over several
// rounds until it calls Done (default 1).
func (m *Manager) Extract(ctx context.Context, msgs []messages.Message, existing []Extracted, maxSteps int) ([]Extracted, error) {
	if m.Model == nil {
		return nil, fmt.Errorf("langmem: Manager.Model is required")
	}
	steps := max(maxSteps, 1)
	order := []string{}
	cur := map[string]Extracted{}
	external := map[string]bool{}
	for _, e := range existing {
		e.Content = normalize(e.Content)
		cur[e.ID] = e
		external[e.ID] = true
		order = append(order, e.ID)
	}
	live := func() []Extracted {
		var out []Extracted
		for _, id := range order {
			if e := cur[id]; !e.Deleted {
				out = append(out, e)
			}
		}
		return out
	}

	session := "<session>\n" + messages.Buffer(msgs) + "\n</session>"
	if steps > 1 {
		session += fmt.Sprintf("\n\nYou have a maximum of %d attempts to form and consolidate memories from this session.", steps-1)
	}
	user := m.cfg.instructions +
		"\n\nEnrich, prune, and organize memories based on any new information. If an existing memory is incorrect or outdated, update it based on the new information. All operations must be done in single parallel multi-tool call. Avoid duplicate extractions.\n\n" +
		renderExisting(live()) + "\n\n" + session
	convo := []messages.Message{messages.System("You are a memory subroutine for an AI."), messages.Human(user)}

	// patchFails counts failed PatchDoc calls per memory. A failure asks for
	// one more round (retry); the second one tells the model to replace the
	// memory instead. The loop cap allows those two extra rounds at most.
	patchFails := map[string]int{}
	for i := 0; i < steps+2; i++ {
		ai, err := m.Model.Generate(ctx, convo, models.WithToolDefinitions(m.toolDefs(live(), i >= 1)...), models.WithToolChoice("any"))
		if err != nil {
			return nil, fmt.Errorf("langmem: extract: %w", err)
		}
		var results []messages.Message
		done, retry := false, false
		for _, tc := range ai.ToolCalls {
			out, isDone, isRetry := m.apply(tc, cur, &order, patchFails)
			done = done || isDone
			retry = retry || isRetry
			results = append(results, messages.Tool(out, tc.ID))
		}
		if done || len(ai.ToolCalls) == 0 || (i >= steps-1 && !retry) {
			break
		}
		convo = append(convo, ai)
		convo = append(convo, results...)
		convo = append(convo, messages.Human("Current memories after your changes:\n"+renderExisting(live())))
	}

	var out []Extracted
	for _, id := range order {
		e := cur[id]
		if e.Deleted && !external[id] {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// apply executes one tool call against the working set. It returns the
// message shown back to the model, whether the model called Done, and whether
// a failed PatchDoc asks for another round.
func (m *Manager) apply(tc messages.ToolCall, cur map[string]Extracted, order *[]string, patchFails map[string]int) (string, bool, bool) {
	args := map[string]any{}
	for k, v := range tc.Args {
		args[k] = v
	}
	id, _ := args[idArg].(string)
	delete(args, idArg)
	known := func() bool { e, ok := cur[id]; return ok && !e.Deleted }

	if tc.Name == patchTool && m.cfg.patchUpdates {
		msg, retry := m.patch(tc.Args, cur, patchFails)
		return msg, false, retry
	}
	switch tc.Name {
	case doneTool:
		return "Done.", true, false
	case removeTool:
		if !m.cfg.deletes {
			return "Error: deleting memories is disabled.", false, false
		}
		if !known() {
			return fmt.Sprintf("Error: memory %q does not exist.", id), false, false
		}
		e := cur[id]
		e.Deleted, e.Content = true, nil
		cur[id] = e
		return fmt.Sprintf("Memory %s deleted.", id), false, false
	}
	s, ok := m.schemaByName(tc.Name)
	if !ok {
		return fmt.Sprintf("Error: unknown tool %q.", tc.Name), false, false
	}
	if err := jsonschema.Validate(s.Parameters, args); err != nil {
		return fmt.Sprintf("Error: invalid %s: %v", s.Name, err), false, false
	}
	switch {
	case id != "" && known():
		if !m.cfg.updates {
			return "Error: updating memories is disabled.", false, false
		}
		delete(patchFails, id)
		cur[id] = Extracted{ID: id, Kind: s.Name, Content: args}
		return fmt.Sprintf("Memory %s updated.", id), false, false
	case m.cfg.inserts:
		nid := messages.NewID()
		cur[nid] = Extracted{ID: nid, Kind: s.Name, Content: args}
		*order = append(*order, nid)
		return fmt.Sprintf("Memory %s inserted.", nid), false, false
	}
	return "Error: inserting memories is disabled; update an existing memory by id.", false, false
}

// patch applies a PatchDoc call to the memory's content. Failures are
// reported to the model; the first one allows a retry, the second one points
// the model at the replacement path (full content with json_doc_id).
func (m *Manager) patch(args map[string]any, cur map[string]Extracted, patchFails map[string]int) (string, bool) {
	id, _ := args[idArg].(string)
	e, ok := cur[id]
	if !ok || e.Deleted {
		return fmt.Sprintf("Error: memory %q does not exist; insert it instead.", id), false
	}
	fallback := fmt.Sprintf("Error: patching %s failed twice. Call the memory tool (%s) with %s %q and the complete new content.", id, e.Kind, idArg, id)
	if patchFails[id] >= 2 {
		return fallback, true
	}
	fail := func(err error) (string, bool) {
		patchFails[id]++
		if patchFails[id] >= 2 {
			return fmt.Sprintf("%s Last error: %v", fallback, err), true
		}
		return fmt.Sprintf("Error: patch failed: %v. Fix the patches and call %s again with %s %q (one retry).", err, patchTool, idArg, id), true
	}
	patches, ok := args["patches"].([]any)
	if !ok {
		return fail(fmt.Errorf("patches must be an array"))
	}
	res, err := jsonpatch.Apply(e.Content, patches)
	if err != nil {
		return fail(err)
	}
	if s, ok := m.schemaByName(e.Kind); ok {
		if err := jsonschema.Validate(s.Parameters, res); err != nil {
			return fail(fmt.Errorf("result does not match %s: %v", e.Kind, err))
		}
	}
	delete(patchFails, id)
	e.Content = res
	cur[id] = e
	return fmt.Sprintf("Memory %s patched.", id), false
}

// ExistingNotes turns plain strings into default-schema memories with fresh
// IDs, for callers that only keep text.
func ExistingNotes(texts ...string) []Extracted {
	out := make([]Extracted, len(texts))
	for i, t := range texts {
		out[i] = Note(messages.NewID(), t)
	}
	return out
}
