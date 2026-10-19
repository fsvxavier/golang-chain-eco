package langmem

import (
	"context"
	"fmt"
	"slices"

	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
	"github.com/fsvxavier/golang-chain-eco/core/tools"
	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// MemorySearcher lets a model decide what to look up: it reads the
// conversation, issues several parallel search_memory queries, and the hits
// are merged and ranked (create_memory_searcher). Use it to fetch context
// before answering.
type MemorySearcher struct {
	Model models.ChatModel
	// Namespace may hold {variables}; empty means DefaultNamespace.
	Namespace Namespace
	Store     store.Store
	// Prompt overrides the default system prompt.
	Prompt string
}

const defaultSearchPrompt = "Search for distinct memories relevant to different aspects of the provided context."

// Search returns the memories relevant to msgs, best first.
func (s MemorySearcher) Search(ctx context.Context, msgs []messages.Message) ([]Hit, error) {
	if s.Model == nil {
		return nil, fmt.Errorf("langmem: MemorySearcher.Model is required")
	}
	prompt := s.Prompt
	if prompt == "" {
		prompt = defaultSearchPrompt
	}
	tool := SearchMemory(ToolConfig{Namespace: s.Namespace, Store: s.Store, Artifact: true})
	in := append([]messages.Message{messages.System(prompt)}, messages.MergeRuns(msgs)...)
	in = append(in, messages.Human("\n\nSearch for memories relevant to the above context."))
	ai, err := s.Model.Generate(ctx, in, models.WithToolDefinitions(tools.Define(tool)), models.WithToolChoice(tool.Name()))
	if err != nil {
		return nil, err
	}
	merged := map[string]Hit{}
	for _, tc := range ai.ToolCalls {
		if tc.Name != tool.Name() {
			continue
		}
		out, err := tool.Call(ctx, tc.Args)
		if err != nil {
			return nil, err
		}
		for _, h := range out.(tools.ContentAndArtifact).Artifact.([]Hit) {
			merged[fmt.Sprint(h.Namespace, "/", h.Key)] = h
		}
	}
	hits := make([]Hit, 0, len(merged))
	for _, h := range merged {
		hits = append(hits, h)
	}
	slices.SortFunc(hits, func(a, b Hit) int { return cmpStr(a.Key, b.Key) })
	return rank(hits, 0), nil
}

func cmpStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// ThreadSummary is the default schema of ExtractThread.
type ThreadSummary struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// ExtractThread summarises a conversation into any schema T
// (create_thread_extractor): a title and summary by default, or e.g. a
// support-ticket record. Handy for episodic memory of whole threads.
func ExtractThread[T any](ctx context.Context, model models.ChatModel, msgs []messages.Message, instructions string) (T, error) {
	if instructions == "" {
		instructions = "You are tasked with summarizing the following conversation."
	}
	return models.Structured[T](model, models.MethodTool).Invoke(ctx, []messages.Message{
		messages.System(instructions),
		messages.Human("Call the provided tool based on the conversation below:\n\n<conversation>" + messages.Buffer(messages.MergeRuns(msgs)) + "</conversation>"),
	})
}
