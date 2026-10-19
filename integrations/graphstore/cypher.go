package graphstore

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/fsvxavier/golang-chain-eco/kag"
)

// ident validates node labels and constraint names: they are interpolated into
// statement text because Cypher cannot parameterize them.
var ident = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// relType validates relationship types, which Cypher cannot take as parameters.
var relType = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// validIdent reports whether s can be interpolated as a label or name.
func validIdent(s string) error {
	if !ident.MatchString(s) {
		return fmt.Errorf("graphstore: invalid identifier %q", s)
	}
	return nil
}

// validRelType keeps interpolated relationship types inert: only upper-case words.
func validRelType(s string) error {
	if !relType.MatchString(s) {
		return fmt.Errorf("graphstore: relation type %q must match ^[A-Z][A-Z0-9_]*$", s)
	}
	return nil
}

// buildPathQuery returns the shortest directed path statement. maxHops is
// interpolated as a number because Cypher requires a literal bound in *1..N.
func buildPathQuery(label string, maxHops int) string {
	return fmt.Sprintf(
		"MATCH p = allShortestPaths((a:%[1]s {id: $from})-[*1..%[2]d]->(b:%[1]s {id: $to})) "+
			"RETURN [r IN relationships(p) | {from: startNode(r).id, type: type(r), to: endNode(r).id, props: r.props, source: r.source}] AS path",
		label, maxHops)
}

// encodeProps stores properties as a JSON string; nil becomes "{}".
func encodeProps(p map[string]any) (string, error) {
	if p == nil {
		return "{}", nil
	}
	b, err := json.Marshal(p)
	return string(b), err
}

// decodeProps reverses encodeProps; empty or null input yields nil.
func decodeProps(s *string) (map[string]any, error) {
	if s == nil || *s == "" || *s == "null" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(*s), &m); err != nil {
		return nil, fmt.Errorf("graphstore: props: %w", err)
	}
	return m, nil
}

// relKey identifies a relation the way kag.Memory does, for dedupe and ordering.
func relKey(from, typ, to string) string { return from + "\x00" + typ + "\x00" + to }

var accentFolder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// unpack decodes one cell of a Query API row; JSON null leaves dst at its zero value.
func unpack(raw json.RawMessage, dst any) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("graphstore: cell %s: %w", raw, err)
	}
	return nil
}

// tokens lowercases, strips basic Latin accents and splits on non-alphanumerics.
// Same rules as kag.Memory so both stores rank names identically.
func tokens(s string) []string {
	return strings.FieldsFunc(accentFolder.Replace(strings.ToLower(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// nameScore is the share of query tokens found in the entity name or ID.
func nameScore(q []string, e kag.Entity) float64 {
	if len(q) == 0 {
		return 0
	}
	have := map[string]bool{}
	for _, t := range append(tokens(e.Name), tokens(e.ID)...) {
		have[t] = true
	}
	hits := 0
	for _, t := range q {
		if have[t] {
			hits++
		}
	}
	return float64(hits) / float64(len(q))
}
