package langmem

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/jsonschema"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/graph"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// ToolConfig configures ManageMemory and SearchMemory.
type ToolConfig struct {
	// Namespace may hold {variables}; empty means DefaultNamespace.
	Namespace Namespace
	// Store defaults to the store attached to the running graph.
	Store store.Store
	// Instructions are appended to the tool description (when to use it).
	Instructions string
	// Name overrides the tool name.
	Name string
	// Schema is the JSON Schema of a memory's content (ManageMemory); default
	// is a plain string. Use jsonschema.For[T]() for typed memories.
	Schema *jsonschema.Schema
	// Actions permitted by ManageMemory (default create, update, delete).
	Actions []string
	// Artifact makes SearchMemory also return the raw hits as the tool
	// artifact (response_format="content_and_artifact").
	Artifact bool
}

func (c ToolConfig) resolve(ctx context.Context) (store.Store, []string, error) {
	st := c.Store
	if st == nil {
		st = graph.StoreFrom(ctx)
	}
	if st == nil {
		return nil, nil, fmt.Errorf("%w: could not get store", ErrConfiguration)
	}
	ns := c.Namespace
	if len(ns) == 0 {
		ns = DefaultNamespace
	}
	r, err := ns.Resolve(ctx)
	return st, r, err
}

// jsonValue normalises v (struct, map, string...) to JSON-like data so every
// store backend can hold it.
func jsonValue(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	return out, json.Unmarshal(b, &out)
}

const manageInstructions = "Proactively call this tool when you:\n\n" +
	"1. Identify a new USER preference.\n" +
	"2. Receive an explicit USER request to remember something or otherwise alter your behavior.\n" +
	"3. Are working and want to record important context.\n" +
	"4. Identify that an existing MEMORY is incorrect or outdated.\n"

// ManageMemory returns the manage_memory tool: the agent creates, updates and
// deletes its own memories during the conversation ("hot path" formation;
// create_manage_memory_tool). Memories are stored as {"content": ...} under a
// UUID key.
func ManageMemory(c ToolConfig) tools.Tool {
	name := c.Name
	if name == "" {
		name = "manage_memory"
	}
	actions := c.Actions
	if len(actions) == 0 {
		actions = []string{"create", "update", "delete"}
	}
	instr := c.Instructions
	if instr == "" {
		instr = manageInstructions
	}
	content := c.Schema
	if content == nil {
		content = &jsonschema.Schema{Type: "string"}
	}
	capital := strings.ToUpper(actions[0][:1]) + actions[0][1:]
	verbs := capital + " a memory"
	if len(actions) > 1 {
		verbs = capital + ", " + strings.Join(actions[1:len(actions)-1], ", ")
		verbs = strings.TrimSuffix(verbs, ", ") + " or " + actions[len(actions)-1] + " a memory"
		if len(actions) == 2 {
			verbs = capital + " or " + actions[1] + " a memory"
		}
	}
	desc := verbs + " to persist across conversations.\n" +
		"Include the MEMORY ID when updating or deleting a MEMORY. Omit when creating a new MEMORY - it will be created for you.\n" + instr
	enum := make([]any, len(actions))
	for i, a := range actions {
		enum[i] = a
	}
	def := actions[0]
	if slices.Contains(actions, "create") {
		def = "create"
	}
	schema := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{
		"content": content,
		"action":  {Type: "string", Enum: enum, Default: def},
		"id":      {Type: "string", Description: "ID of an existing memory to update or delete"},
	}}
	return tools.NewRaw(name, desc, schema, func(ctx context.Context, a map[string]any) (any, error) {
		st, ns, err := c.resolve(ctx)
		if err != nil {
			return nil, err
		}
		action, _ := a["action"].(string)
		if action == "" {
			action = def
		}
		id, _ := a["id"].(string)
		if !slices.Contains(actions, action) {
			return nil, tools.Errorf("Invalid action %s. Must be one of %v.", action, actions)
		}
		if action == "create" && id != "" {
			return nil, tools.Errorf("You cannot provide a MEMORY ID when creating a MEMORY. Please try again, omitting the id argument.")
		}
		if action != "create" && id == "" {
			return nil, tools.Errorf("You must provide a MEMORY ID when deleting or updating a MEMORY.")
		}
		if action == "delete" {
			if err := st.Delete(ctx, ns, id); err != nil {
				return nil, err
			}
			return "Deleted memory " + id, nil
		}
		if id == "" {
			id = messages.NewID()
		}
		v, err := jsonValue(a["content"])
		if err != nil {
			return nil, err
		}
		if err := st.Put(ctx, ns, id, map[string]any{"content": v}); err != nil {
			return nil, err
		}
		return fmt.Sprintf("%sd memory %s", action, id), nil
	})
}

func hitDict(h Hit) map[string]any {
	return map[string]any{"namespace": h.Namespace, "key": h.Key, "value": h.Value,
		"created_at": h.CreatedAt, "updated_at": h.UpdatedAt, "score": h.Score}
}

// SearchMemory returns the search_memory tool (create_search_memory_tool).
// Results are ranked semantically when the store is an IndexedStore.
func SearchMemory(c ToolConfig) tools.Tool {
	name := c.Name
	if name == "" {
		name = "search_memory"
	}
	schema := &jsonschema.Schema{Type: "object", Required: []string{"query"}, Properties: map[string]*jsonschema.Schema{
		"query":  {Type: "string", Description: "Search query"},
		"limit":  {Type: "integer", Description: "Max results (default 10)"},
		"offset": {Type: "integer", Description: "Results to skip, for paging (default 0)"},
		"filter": {Type: "object", Description: "Exact-match filter on memory value fields"},
	}}
	return tools.NewRaw(name, "Search your long-term memories for information relevant to your current context. "+c.Instructions, schema,
		func(ctx context.Context, a map[string]any) (any, error) {
			st, ns, err := c.resolve(ctx)
			if err != nil {
				return nil, err
			}
			q, _ := a["query"].(string)
			limit, offset := 10, 0
			if f, ok := a["limit"].(float64); ok && f > 0 {
				limit = int(f)
			} else if i, ok := a["limit"].(int); ok && i > 0 {
				limit = i
			}
			if f, ok := a["offset"].(float64); ok && f > 0 {
				offset = int(f)
			} else if i, ok := a["offset"].(int); ok && i > 0 {
				offset = i
			}
			filter, _ := a["filter"].(map[string]any)
			hits, err := Query(ctx, st, ns, q, filter, limit+offset)
			if err != nil {
				return nil, err
			}
			hits = hits[min(offset, len(hits)):]
			dicts := make([]map[string]any, len(hits))
			for i, h := range hits {
				dicts[i] = hitDict(h)
			}
			b, err := json.Marshal(dicts)
			if err != nil {
				return nil, err
			}
			if c.Artifact {
				return tools.ContentAndArtifact{Content: string(b), Artifact: hits}, nil
			}
			return string(b), nil
		})
}

func searchDefinition(instructions, name string) tools.Definition {
	return tools.Define(SearchMemory(ToolConfig{Instructions: instructions, Name: name}))
}
