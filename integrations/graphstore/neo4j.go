// Package graphstore implements kag.GraphStore on graph databases over their
// HTTP query APIs. Library code is stdlib-only.
package graphstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/core/embeddings"
	"github.com/fsvxavier/golang-chain-eco/kag"
)

var _ kag.GraphStore = (*Neo4j)(nil)

// Neo4j keeps the graph in the Neo4j Query API (HTTP, 5.19+ / Aura).
// Entities are (:Label {id,name,type,props,vector}) nodes and relations are
// edges whose type is the relation type. Nodes created only as relation
// endpoints carry entity=false and are invisible to entity lookups.
type Neo4j struct {
	URL      string // http://localhost:7474 or the Aura query API host
	Username string
	Password string
	Database string // default "neo4j"
	Label    string // node label, default "Entity"
	HTTP     *http.Client
}

func (n *Neo4j) label() (string, error) {
	l := n.Label
	if l == "" {
		l = "Entity"
	}
	return l, validIdent(l)
}

func constraintName(label string) string { return "lcg_" + label + "_id" }

// run executes one statement and returns its rows.
func (n *Neo4j) run(ctx context.Context, stmt string, params map[string]any) ([][]json.RawMessage, error) {
	db := n.Database
	if db == "" {
		db = "neo4j"
	}
	body, _ := json.Marshal(map[string]any{"statement": stmt, "parameters": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(n.URL, "/")+"/db/"+url.PathEscape(db)+"/query/v2", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(n.Username, n.Password)
	hc := n.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Data struct {
			Values [][]json.RawMessage `json:"values"`
		} `json:"data"`
		Errors []struct{ Code, Message string } `json:"errors"`
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Errors) > 0 {
		return nil, fmt.Errorf("neo4j: %s: %s", out.Errors[0].Code, out.Errors[0].Message)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("neo4j: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return out.Data.Values, nil
}

func (n *Neo4j) UpsertEntities(ctx context.Context, ents []kag.Entity) error {
	if len(ents) == 0 {
		return nil
	}
	label, err := n.label()
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
	if _, err := n.run(ctx, fmt.Sprintf("CREATE CONSTRAINT %s IF NOT EXISTS FOR (n:%s) REQUIRE n.id IS UNIQUE", constraintName(label), label), nil); err != nil {
		return err
	}
	_, err = n.run(ctx, fmt.Sprintf("UNWIND $rows AS r MERGE (n:%s {id: r.id}) SET n.entity = true, n.name = r.name, n.type = r.type, n.props = r.props, n.vector = r.vector", label),
		map[string]any{"rows": rows})
	return err
}

func (n *Neo4j) UpsertRelations(ctx context.Context, rels []kag.Relation) error {
	if len(rels) == 0 {
		return nil
	}
	label, err := n.label()
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
		if _, err := n.run(ctx, stmt, map[string]any{"rows": rows}); err != nil {
			return err
		}
	}
	return nil
}

func (n *Neo4j) Neighbors(ctx context.Context, id string, relTypes []string, dir kag.Direction) ([]kag.Relation, error) {
	label, err := n.label()
	if err != nil {
		return nil, err
	}
	if relTypes == nil {
		relTypes = []string{}
	}
	rows, err := n.run(ctx, fmt.Sprintf(
		"MATCH (x:%[1]s {id: $id})-[r]-(y:%[1]s) WHERE size($types) = 0 OR type(r) IN $types "+
			"RETURN startNode(r).id, type(r), endNode(r).id, r.props, r.source", label),
		map[string]any{"id": id, "types": relTypes})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var res []kag.Relation
	for _, row := range rows {
		if len(row) < 5 {
			continue
		}
		var rel kag.Relation
		var props, source *string
		if err := unpack(row[0], &rel.From); err != nil {
			return nil, err
		}
		if err := unpack(row[1], &rel.Type); err != nil {
			return nil, err
		}
		if err := unpack(row[2], &rel.To); err != nil {
			return nil, err
		}
		if err := unpack(row[3], &props); err != nil {
			return nil, err
		}
		if err := unpack(row[4], &source); err != nil {
			return nil, err
		}
		if (dir == kag.Out && rel.From != id) || (dir == kag.In && rel.To != id) {
			continue
		}
		k := relKey(rel.From, rel.Type, rel.To)
		if seen[k] {
			continue
		}
		seen[k] = true
		if rel.Props, err = decodeProps(props); err != nil {
			return nil, err
		}
		if source != nil {
			rel.Source = *source
		}
		res = append(res, rel)
	}
	slices.SortFunc(res, func(a, b kag.Relation) int {
		return strings.Compare(relKey(a.From, a.Type, a.To), relKey(b.From, b.Type, b.To))
	})
	return res, nil
}

func (n *Neo4j) Entity(ctx context.Context, id string) (kag.Entity, bool, error) {
	label, err := n.label()
	if err != nil {
		return kag.Entity{}, false, err
	}
	rows, err := n.run(ctx, fmt.Sprintf("MATCH (n:%s {id: $id}) WHERE n.entity = true RETURN n.id, n.name, n.type, n.props, n.vector", label),
		map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return kag.Entity{}, false, err
	}
	e, err := entityFromRow(rows[0])
	return e, err == nil, err
}

// FindEntities scores candidates in Go, with the same rules as kag.Memory:
// cosine when both vectors exist, otherwise the share of query tokens found in
// name or ID. ponytail: reads every entity per query; push cosine into
// vector.similarity.cosine and name match into an index when the graph grows.
func (n *Neo4j) FindEntities(ctx context.Context, text string, vec []float32, k int) ([]kag.Entity, error) {
	label, err := n.label()
	if err != nil {
		return nil, err
	}
	rows, err := n.run(ctx, fmt.Sprintf("MATCH (n:%s) WHERE n.entity = true RETURN n.id, n.name, n.type, n.props, n.vector", label), nil)
	if err != nil {
		return nil, err
	}
	q := tokens(text)
	type scored struct {
		e kag.Entity
		s float64
	}
	var hits []scored
	for _, row := range rows {
		e, err := entityFromRow(row)
		if err != nil {
			return nil, err
		}
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

func (n *Neo4j) Path(ctx context.Context, from, to string, maxHops int) ([][]kag.Relation, error) {
	label, err := n.label()
	if err != nil {
		return nil, err
	}
	if maxHops <= 0 || from == to {
		return nil, nil
	}
	rows, err := n.run(ctx, buildPathQuery(label, maxHops), map[string]any{"from": from, "to": to})
	if err != nil {
		return nil, err
	}
	var paths [][]kag.Relation
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		var hops []struct {
			From   string  `json:"from"`
			Type   string  `json:"type"`
			To     string  `json:"to"`
			Props  *string `json:"props"`
			Source *string `json:"source"`
		}
		if err := json.Unmarshal(row[0], &hops); err != nil {
			return nil, fmt.Errorf("neo4j: path row: %w", err)
		}
		path := make([]kag.Relation, 0, len(hops))
		for _, h := range hops {
			props, err := decodeProps(h.Props)
			if err != nil {
				return nil, err
			}
			r := kag.Relation{From: h.From, Type: h.Type, To: h.To, Props: props}
			if h.Source != nil {
				r.Source = *h.Source
			}
			path = append(path, r)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (n *Neo4j) Delete(ctx context.Context, ids []string) error {
	label, err := n.label()
	if err != nil || len(ids) == 0 {
		return err
	}
	_, err = n.run(ctx, fmt.Sprintf("UNWIND $ids AS id MATCH (n:%s {id: id}) DETACH DELETE n", label), map[string]any{"ids": ids})
	return err
}

// entityFromRow reads (id, name, type, props, vector) as returned by the entity queries.
func entityFromRow(row []json.RawMessage) (kag.Entity, error) {
	if len(row) < 5 {
		return kag.Entity{}, errors.New("neo4j: short entity row")
	}
	var e kag.Entity
	var props *string
	var vec []float32
	if err := unpack(row[0], &e.ID); err != nil {
		return e, err
	}
	if err := unpack(row[1], &e.Name); err != nil {
		return e, err
	}
	if err := unpack(row[2], &e.Type); err != nil {
		return e, err
	}
	if err := unpack(row[3], &props); err != nil {
		return e, err
	}
	if err := unpack(row[4], &vec); err != nil {
		return e, err
	}
	var err error
	if e.Props, err = decodeProps(props); err != nil {
		return e, err
	}
	if len(vec) > 0 {
		e.Vector = vec
	}
	return e, nil
}
