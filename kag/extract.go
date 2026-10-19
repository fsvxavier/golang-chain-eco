package kag

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/documents"
	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/core/messages"
	"github.com/fsvxavier/golang-chain-eco/core/models"
)

// Schema constrains extraction. Empty lists mean free-form.
type Schema struct {
	EntityTypes   []string
	RelationTypes []string
}

// Extractor turns documents into entities and relations with an LLM and stores
// them in Graph. Embedder is optional; without it entities carry no vectors.
type Extractor struct {
	Model    models.ChatModel
	Embedder embeddings.Embedder
	Schema   Schema
	Graph    GraphStore
	Method   models.StructuredMethod
}

// ExtractStats counts what was stored and what was discarded.
type ExtractStats struct{ Entities, Relations, Dropped int }

// extraction is the structured output requested from the model for each chunk.
type extraction struct {
	Entities  []extractedEntity   `json:"entities"`
	Relations []extractedRelation `json:"relations"`
}

type extractedEntity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type extractedRelation struct {
	From string `json:"from"`
	Type string `json:"type"`
	To   string `json:"to"`
}

// Extract runs the model over each document and upserts the resulting graph.
// Chunks that fail are skipped; their errors are joined and returned together
// with the stats for the chunks that succeeded.
// ponytail: sequential calls; parallelize with a bounded worker pool if LLM latency dominates.
func (x *Extractor) Extract(ctx context.Context, docs []documents.Document) (ExtractStats, error) {
	if x.Model == nil || x.Graph == nil {
		return ExtractStats{}, errors.New("kag: extractor needs Model and Graph")
	}
	method := x.Method
	if method == "" {
		method = models.MethodTool
	}
	run := models.Structured[extraction](x.Model, method)

	allowed := map[string]bool{}
	for _, t := range x.Schema.RelationTypes {
		allowed[relType(t)] = true
	}

	var (
		stats   ExtractStats
		errs    []error
		ents    []Entity
		entIdx  = map[string]int{}
		rels    []Relation
		relSeen = map[string]bool{}
	)
	// ensure adds an entity referenced only by a relation, so the edge has ends.
	ensure := func(id, name string) {
		if _, ok := entIdx[id]; !ok {
			entIdx[id] = len(ents)
			ents = append(ents, Entity{ID: id, Name: strings.TrimSpace(name)})
		}
	}

	for i, doc := range docs {
		text := strings.TrimSpace(doc.PageContent)
		if text == "" {
			continue
		}
		out, err := run.Invoke(ctx, []messages.Message{
			messages.System(extractPrompt(x.Schema)),
			messages.Human(text),
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("kag: extract chunk %d: %w", i, err))
			continue
		}

		for _, e := range out.Entities {
			id := NormalizeID(e.Name)
			if id == "" {
				stats.Dropped++
				continue
			}
			if j, ok := entIdx[id]; ok {
				// Duplicate, but it may upgrade an implicit entity with its type.
				if ents[j].Type == "" {
					ents[j].Type = strings.TrimSpace(e.Type)
				}
				stats.Dropped++
				continue
			}
			ensure(id, e.Name)
			ents[entIdx[id]].Type = strings.TrimSpace(e.Type)
		}

		src := sourceOf(doc)
		for _, r := range out.Relations {
			from, to, typ := NormalizeID(r.From), NormalizeID(r.To), relType(r.Type)
			if from == "" || to == "" || typ == "" || (len(allowed) > 0 && !allowed[typ]) {
				stats.Dropped++
				continue
			}
			key := relKey(Relation{From: from, Type: typ, To: to})
			if relSeen[key] {
				stats.Dropped++
				continue
			}
			relSeen[key] = true
			ensure(from, r.From)
			ensure(to, r.To)
			rels = append(rels, Relation{From: from, Type: typ, To: to, Source: src})
		}
	}

	if x.Embedder != nil && len(ents) > 0 {
		names := make([]string, len(ents))
		for i, e := range ents {
			names[i] = e.Name
		}
		vecs, err := x.Embedder.EmbedDocuments(ctx, names)
		if err == nil && len(vecs) != len(ents) {
			err = fmt.Errorf("embedder returned %d vectors for %d entities", len(vecs), len(ents))
		}
		if err != nil {
			return stats, errors.Join(append(errs, fmt.Errorf("kag: embed entities: %w", err))...)
		}
		for i := range ents {
			ents[i].Vector = vecs[i]
		}
	}

	if err := x.Graph.UpsertEntities(ctx, ents); err != nil {
		return stats, errors.Join(append(errs, err)...)
	}
	if err := x.Graph.UpsertRelations(ctx, rels); err != nil {
		return stats, errors.Join(append(errs, err)...)
	}
	stats.Entities, stats.Relations = len(ents), len(rels)
	return stats, errors.Join(errs...)
}

// relType turns free text from the model into a Cypher-safe relationship type,
// matching ^[A-Z][A-Z0-9_]*$. Every other byte becomes '_', repeats collapse,
// edge underscores are trimmed. It returns "" when nothing usable is left or
// the result would start with a digit. Relation types reach Cypher, so this is
// the only place they are shaped.
func relType(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z':
			c -= 'a' - 'A'
		case 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		default:
			c = '_'
		}
		if c == '_' && (len(b) == 0 || b[len(b)-1] == '_') {
			continue
		}
		b = append(b, c)
	}
	for len(b) > 0 && b[len(b)-1] == '_' {
		b = b[:len(b)-1]
	}
	if len(b) == 0 || ('0' <= b[0] && b[0] <= '9') {
		return ""
	}
	return string(b)
}

func sourceOf(d documents.Document) string {
	if d.ID != "" {
		return d.ID
	}
	s, _ := d.Metadata["source"].(string)
	return s
}

func extractPrompt(s Schema) string {
	var b strings.Builder
	b.WriteString("Extract a knowledge graph from the user's text. Entities are named things; relations connect two entities. Use names exactly as they appear in the text. Relation types are short verbs or phrases.")
	if len(s.EntityTypes) > 0 {
		fmt.Fprintf(&b, " Entity types: %s.", strings.Join(s.EntityTypes, ", "))
	}
	if len(s.RelationTypes) > 0 {
		fmt.Fprintf(&b, " Use only these relation types: %s.", strings.Join(s.RelationTypes, ", "))
	}
	return b.String()
}
