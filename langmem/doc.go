// Package langmem is a Go port of LangMem (github.com/langchain-ai/langmem):
// long-term memory for agents, built on the langgraph store.
//
// Memory types and where they live:
//
//   - Semantic (facts, preferences): Manager / StoreManager with a collection
//     of Memory items, or a single profile document (WithInserts(false) +
//     WithDefault).
//   - Episodic (past experiences): StoreManager with EpisodeSchema.
//   - Procedural (how to behave): PromptOptimizer and MultiPromptOptimizer.
//
// Forming memories:
//
//   - Hot path: ManageMemory and SearchMemory tools the agent calls itself.
//   - Background: StoreManager.Invoke, debounced by ReflectionExecutor.
//
// Short-term memory: SummarizeMessages, SummarizationNode and
// SummarizationMiddleware keep a long conversation inside a token budget.
//
// Storage: any store.Store; wrap it in IndexedStore for semantic search.
// Namespaces such as ("memories", "{langgraph_user_id}") are filled from
// runnables.Config.Configurable. Middleware plugs the lot into agents.Create.
//
// See docs/LANGMEM.md.
package langmem
