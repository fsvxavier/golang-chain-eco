// Package kag is the knowledge-graph side of RAG/CAG/KAG: entities and typed
// relations extracted from documents, a GraphStore to keep them, and (in later
// files) an LLM triple extractor and a multi-hop engine.
package kag

import (
	"context"
	"strings"
)

// Entity is a node of the graph. Vector is optional and enables cosine lookup.
type Entity struct {
	ID     string
	Type   string
	Name   string
	Props  map[string]any
	Vector []float32
}

// Relation is a directed, typed edge. Source identifies the chunk or document
// the triple came from.
type Relation struct {
	From   string
	Type   string
	To     string
	Props  map[string]any
	Source string
}

// Direction selects which edges Neighbors follows.
type Direction int

const (
	Out Direction = iota
	In
	Both
)

// GraphStore is the persistence contract for the knowledge graph. Memory is the
// reference implementation; Neo4j and Memgraph adapters must satisfy it too.
type GraphStore interface {
	UpsertEntities(ctx context.Context, ents []Entity) error
	UpsertRelations(ctx context.Context, rels []Relation) error
	// Neighbors returns the relations touching id in dir; empty relTypes means all.
	Neighbors(ctx context.Context, id string, relTypes []string, dir Direction) ([]Relation, error)
	// FindEntities ranks entities by cosine when vec and Entity.Vector exist,
	// otherwise by normalized name match.
	FindEntities(ctx context.Context, text string, vec []float32, k int) ([]Entity, error)
	Entity(ctx context.Context, id string) (Entity, bool, error)
	// Path returns every shortest directed path from->to of at most maxHops edges.
	Path(ctx context.Context, from, to string, maxHops int) ([][]Relation, error)
	// Delete removes the entities and every relation that touches them.
	Delete(ctx context.Context, ids []string) error
}

// NormalizeID lowercases s, trims it and turns runs of whitespace into "_".
func NormalizeID(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "_")
}

var accentFolder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)
