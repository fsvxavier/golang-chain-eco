package graphstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/integrations/memgraph"
	"github.com/fsvxavier/golang-chain-eco/kag"
)

var _ kag.GraphStore = (*Memgraph)(nil)

// Memgraph keeps the graph in Memgraph over Bolt (integrations/memgraph).
// The data model is the Neo4j one: (:Label {id,name,type,props,vector,entity})
// nodes and typed relationships. Unlike Neo4j, the path query uses
// [*ALLSHORTEST ..N (r, n | 1) w]: allShortestPaths() does not parse here, and
// [*BFS ..N] returns only one of the equal-length paths.
type Memgraph struct {
	Client *memgraph.Client
	Label  string // node label, default "Entity"
}

func (m *Memgraph) label() (string, error) {
	l := m.Label
	if l == "" {
		l = "Entity"
	}
	return l, validIdent(l)
}

// run executes one statement and returns its rows (values as produced by unpack).
func (m *Memgraph) run(ctx context.Context, stmt string, params map[string]any) ([][]any, error) {
	if m.Client == nil {
		return nil, errors.New("graphstore: Memgraph.Client is nil")
	}
	_, rows, err := m.Client.Run(ctx, stmt, params)
	return rows, err
}

func (m *Memgraph) UpsertEntities(ctx context.Context, ents []kag.Entity) error {
	if len(ents) == 0 {
		return nil
	}
	label, err := m.label()
	if err != nil {
		return err
	}
	rows := make([]map[string]any, len(ents))
	for i, e := range ents {
		if e.ID == "" {
			return errors.New("kag: entity without ID")
		}
		props, err := encodeProps(e.Props)
		if err != nil {
			return err
		}
		rows[i] = map[string]any{"id": e.ID, "name": e.Name, "type": e.Type, "props": props, "vector": e.Vector}
	}
	// Idempotent on Memgraph: repeating CREATE CONSTRAINT is not an error.
	if _, err := m.run(ctx, fmt.Sprintf("CREATE CONSTRAINT ON (n:%s) ASSERT n.id IS UNIQUE", label), nil); err != nil {
		return err
	}
	_, err = m.run(ctx, fmt.Sprintf("UNWIND $rows AS r MERGE (n:%s {id: r.id}) SET n.entity = true, n.name = r.name, n.type = r.type, n.props = r.props, n.vector = r.vector", label),
		map[string]any{"rows": rows})
	return err
}

func (m *Memgraph) UpsertRelations(ctx context.Context, rels []kag.Relation) error {
	if len(rels) == 0 {
		return nil
	}
	label, err := m.label()
	if err != nil {
		return err
	}
	// Validate everything before the first request so a bad type sends nothing.
	byType := map[string][]map[string]any{}
	for _, r := range rels {
		if r.From == "" || r.To == "" || r.Type == "" {
			return fmt.Errorf("kag: relation needs From, Type and To: %+v", r)
		}
		if err := validRelType(r.Type); err != nil {
			return err
		}
		props, err := encodeProps(r.Props)
		if err != nil {
			return err
		}
		byType[r.Type] = append(byType[r.Type], map[string]any{"from": r.From, "to": r.To, "props": props, "source": r.Source})
	}
	for typ, rows := range byType {
		// The relation type is interpolated (validated above); everything else is a parameter.
		stmt := fmt.Sprintf("UNWIND $rows AS r "+
			"MERGE (a:%[1]s {id: r.from}) ON CREATE SET a.entity = false "+
			"MERGE (b:%[1]s {id: r.to}) ON CREATE SET b.entity = false "+
			"MERGE (a)-[e:%[2]s]->(b) SET e.props = r.props, e.source = r.source", label, typ)
		if _, err := m.run(ctx, stmt, map[string]any{"rows": rows}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Memgraph) Neighbors(ctx context.Context, id string, relTypes []string, dir kag.Direction) ([]kag.Relation, error) {
	label, err := m.label()
	if err != nil {
		return nil, err
	}
	if relTypes == nil {
		relTypes = []string{}
	}
	rows, err := m.run(ctx, fmt.Sprintf(
		"MATCH (x:%[1]s {id: $id})-[r]-(y:%[1]s) WHERE size($types) = 0 OR type(r) IN $types "+
			"RETURN startNode(r).id, type(r), endNode(r).id, r.props, r.source", label),
		map[string]any{"id": id, "types": relTypes})
	if err != nil {
		return nil, err
	}
	all, err := rowsToRelations(rows)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var res []kag.Relation
	for _, rel := range all {
		if (dir == kag.Out && rel.From != id) || (dir == kag.In && rel.To != id) {
			continue
		}
		k := relKey(rel.From, rel.Type, rel.To)
		if seen[k] {
			continue
		}
		seen[k] = true
		res = append(res, rel)
	}
	slices.SortFunc(res, func(a, b kag.Relation) int {
		return strings.Compare(relKey(a.From, a.Type, a.To), relKey(b.From, b.Type, b.To))
	})
	return res, nil
}

func (m *Memgraph) Entity(ctx context.Context, id string) (kag.Entity, bool, error) {
	label, err := m.label()
	if err != nil {
		return kag.Entity{}, false, err
	}
	rows, err := m.run(ctx, fmt.Sprintf("MATCH (n:%s {id: $id}) WHERE n.entity = true RETURN n.id, n.name, n.type, n.props, n.vector", label),
		map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return kag.Entity{}, false, err
	}
	e, err := entityFromMemgraphRow(rows[0])
	return e, err == nil, err
}

// FindEntities scores candidates in Go with the same rules as kag.Memory and
// Neo4j: cosine when both vectors exist, otherwise name-token share.
// ponytail: reads every entity per query; push cosine into vector search when the graph grows.
func (m *Memgraph) FindEntities(ctx context.Context, text string, vec []float32, k int) ([]kag.Entity, error) {
	label, err := m.label()
	if err != nil {
		return nil, err
	}
	rows, err := m.run(ctx, fmt.Sprintf("MATCH (n:%s) WHERE n.entity = true RETURN n.id, n.name, n.type, n.props, n.vector", label), nil)
	if err != nil {
		return nil, err
	}
	ents, err := rowsToEntities(rows)
	if err != nil {
		return nil, err
	}
	q := tokens(text)
	type scored struct {
		e kag.Entity
		s float64
	}
	var hits []scored
	for _, e := range ents {
		var s float64
		if len(vec) > 0 && len(e.Vector) > 0 {
			s = embeddings.Cosine(vec, e.Vector)
		} else {
			s = nameScore(q, e)
		}
		if s > 0 {
			hits = append(hits, scored{e, s})
		}
	}
	slices.SortFunc(hits, func(a, b scored) int {
		if a.s != b.s {
			if a.s > b.s {
				return -1
			}
			return 1
		}
		return strings.Compare(a.e.ID, b.e.ID)
	})
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	res := make([]kag.Entity, len(hits))
	for i, h := range hits {
		res[i] = h.e
	}
	return res, nil
}

// memgraphPathQuery returns every shortest directed path of at most maxHops.
// maxHops is interpolated as a literal because the bound must be a number.
func memgraphPathQuery(label string, maxHops int) string {
	return fmt.Sprintf(
		"MATCH p = (a:%[1]s {id: $from})-[*ALLSHORTEST ..%[2]d (r, n | 1) w]->(b:%[1]s {id: $to}) "+
			"RETURN [r IN relationships(p) | {from: startNode(r).id, type: type(r), to: endNode(r).id, props: r.props, source: r.source}] AS path",
		label, maxHops)
}

func (m *Memgraph) Path(ctx context.Context, from, to string, maxHops int) ([][]kag.Relation, error) {
	label, err := m.label()
	if err != nil {
		return nil, err
	}
	if maxHops <= 0 || from == to {
		return nil, nil
	}
	rows, err := m.run(ctx, memgraphPathQuery(label, maxHops), map[string]any{"from": from, "to": to})
	if err != nil {
		return nil, err
	}
	var paths [][]kag.Relation
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		path, err := pathFromValue(row[0])
		if err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (m *Memgraph) Delete(ctx context.Context, ids []string) error {
	label, err := m.label()
	if err != nil || len(ids) == 0 {
		return err
	}
	_, err = m.run(ctx, fmt.Sprintf("UNWIND $ids AS id MATCH (n:%s {id: id}) DETACH DELETE n", label), map[string]any{"ids": ids})
	return err
}

// rowsToEntities converts rows of (id, name, type, props, vector).
func rowsToEntities(rows [][]any) ([]kag.Entity, error) {
	out := make([]kag.Entity, 0, len(rows))
	for _, row := range rows {
		e, err := entityFromMemgraphRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// entityFromMemgraphRow reads (id, name, type, props, vector); nil cells stay zero.
func entityFromMemgraphRow(row []any) (kag.Entity, error) {
	if len(row) < 5 {
		return kag.Entity{}, errors.New("graphstore: short entity row")
	}
	var e kag.Entity
	var err error
	if e.ID, err = str(row[0]); err != nil {
		return e, err
	}
	if e.Name, err = str(row[1]); err != nil {
		return e, err
	}
	if e.Type, err = str(row[2]); err != nil {
		return e, err
	}
	if e.Props, err = propsValue(row[3]); err != nil {
		return e, err
	}
	if e.Vector, err = vectorValue(row[4]); err != nil {
		return e, err
	}
	return e, nil
}

// rowsToRelations converts rows of (from, type, to, props, source).
func rowsToRelations(rows [][]any) ([]kag.Relation, error) {
	out := make([]kag.Relation, 0, len(rows))
	for _, row := range rows {
		if len(row) < 5 {
			return nil, errors.New("graphstore: short relation row")
		}
		var r kag.Relation
		var err error
		if r.From, err = str(row[0]); err != nil {
			return nil, err
		}
		if r.Type, err = str(row[1]); err != nil {
			return nil, err
		}
		if r.To, err = str(row[2]); err != nil {
			return nil, err
		}
		if r.Props, err = propsValue(row[3]); err != nil {
			return nil, err
		}
		if r.Source, err = str(row[4]); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// pathFromValue converts one path value: a list of hop maps
// {from, type, to, props, source}.
func pathFromValue(v any) ([]kag.Relation, error) {
	if v == nil {
		return nil, nil
	}
	hops, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("graphstore: path is %T, want list", v)
	}
	path := make([]kag.Relation, 0, len(hops))
	for _, h := range hops {
		m, ok := h.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("graphstore: hop is %T, want map", h)
		}
		rel, err := relationFromHop(m)
		if err != nil {
			return nil, err
		}
		path = append(path, rel)
	}
	return path, nil
}

func relationFromHop(m map[string]any) (kag.Relation, error) {
	var r kag.Relation
	var err error
	if r.From, err = str(m["from"]); err != nil {
		return r, err
	}
	if r.Type, err = str(m["type"]); err != nil {
		return r, err
	}
	if r.To, err = str(m["to"]); err != nil {
		return r, err
	}
	if r.Props, err = propsValue(m["props"]); err != nil {
		return r, err
	}
	if r.Source, err = str(m["source"]); err != nil {
		return r, err
	}
	return r, nil
}

// str accepts a string or nil (absent value).
func str(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	}
	return "", fmt.Errorf("graphstore: value %v (%T) is not a string", v, v)
}

// propsValue decodes the JSON string that encodeProps wrote; nil yields nil.
func propsValue(v any) (map[string]any, error) {
	s, err := str(v)
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, nil
	}
	return decodeProps(&s)
}

// vectorValue accepts a list of numbers (Bolt delivers float64 or int64) or nil.
func vectorValue(v any) ([]float32, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("graphstore: vector is %T, want list", v)
	}
	out := make([]float32, 0, len(list))
	for _, x := range list {
		switch n := x.(type) {
		case float64:
			out = append(out, float32(n))
		case int64:
			out = append(out, float32(n))
		default:
			return nil, fmt.Errorf("graphstore: vector element %v (%T) is not a number", x, x)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
