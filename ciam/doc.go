// Package ciam is a Go port of the ideas of Context IA Manager
// (github.com/smkbarbosa/context-ia-manager): give an AI assistant precise,
// cheap context about a codebase instead of whole files.
//
//   - Index: walks a project, splits files into typed chunks (model, view,
//     test, adr, prd...), embeds them and stores them incrementally (only
//     changed files are re-embedded).
//   - Search: hybrid ranking (vector similarity + BM25 keyword scoring fused
//     with Reciprocal Rank Fusion), filterable by chunk type, with caching and
//     optional compression to a code skeleton.
//   - Knowledge: ADR, PRD and implementation Plan documents under docs/,
//     created from templates, versioned with the code and searchable, so the
//     assistant learns the "why" and not only the "how"; DecisionContext
//     returns code + ADR + PRD + plans in one call.
//   - Memory: remember/recall notes across sessions, on top of langmem.
//   - Tools and MCP: the same capabilities as tools.Tool values for in-process
//     agents and as an MCP server (mcp/server) for VS Code, Cursor, etc.
//
// The original is Django/Python oriented; the classifier here is pluggable
// and ships with Django, Go, Python, JS/TS and generic rules.
package ciam
