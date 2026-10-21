package compress

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// TOONOptions configures TOONFromJSON. The zero value uses a comma delimiter.
type TOONOptions struct {
	// Delimiter separates array values and tabular cells: "," (default),
	// "\t" or "|". Any other value is an error.
	Delimiter string
}

var (
	toonKeyPlain   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	toonNumericLik = regexp.MustCompile(`(?i)^[+-]?[0-9]+(\.[0-9]+)?(e[+-]?[0-9]+)?$`)
)

// toonObject is a JSON object that keeps its key order.
type toonObject struct {
	keys []string
	vals []any
}

// TOONFromJSON encodes JSON text as TOON (spec v4.4, the subset without key
// folding, keyed tabular form or decoding). Object key order is preserved and
// numbers keep their digits through json.Number.
func TOONFromJSON(text string, o TOONOptions) (string, error) {
	delim, marker, err := toonDelimiter(o.Delimiter)
	if err != nil {
		return "", err
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	root, err := toonParse(dec)
	if err != nil {
		return "", fmt.Errorf("compress: invalid JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", errors.New("compress: invalid JSON: trailing data after value")
	}
	e := &toonEncoder{delim: delim, marker: marker}
	switch v := root.(type) {
	case *toonObject:
		e.fields(v, 0, "")
	case []any:
		e.array("", "", v, 0, true)
	default:
		s := e.prim(v)
		if str, ok := v.(string); ok && strings.HasPrefix(str, "\uFEFF") {
			s = toonEscapeQuoted(str)
		}
		e.lines = append(e.lines, s)
	}
	return strings.Join(e.lines, "\n"), nil
}

// TOONShorter returns the default TOON encoding of text and true when text is
// a JSON object or array and the TOON form is strictly shorter. Otherwise it
// returns text and false.
func TOONShorter(text string) (string, bool) {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return text, false
	}
	out, err := TOONFromJSON(text, TOONOptions{})
	// an empty document (root {}) is not a usable rewrite
	if err != nil || out == "" || len(out) >= len(text) {
		return text, false
	}
	return out, true
}

func toonDelimiter(d string) (delim, marker string, err error) {
	switch d {
	case "", ",":
		return ",", "", nil
	case "\t":
		return "\t", "\t", nil
	case "|":
		return "|", "|", nil
	}
	return "", "", fmt.Errorf("compress: unsupported TOON delimiter %q", d)
}

func toonParse(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return toonParseTok(dec, tok)
}

func toonParseTok(dec *json.Decoder, tok json.Token) (any, error) {
	d, isDelim := tok.(json.Delim)
	if !isDelim {
		return tok, nil
	}
	switch d {
	case '{':
		obj := &toonObject{}
		// Duplicate keys follow encoding/json: the last value wins, at the
		// position of the first occurrence. A TOON strict decoder rejects a
		// repeated sibling key, so the encoder never emits one.
		pos := make(map[string]int)
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := kt.(string)
			if !ok {
				return nil, errors.New("object key is not a string")
			}
			v, err := toonParse(dec)
			if err != nil {
				return nil, err
			}
			if i, dup := pos[key]; dup {
				obj.vals[i] = v
				continue
			}
			pos[key] = len(obj.keys)
			obj.keys = append(obj.keys, key)
			obj.vals = append(obj.vals, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		arr := []any{}
		for dec.More() {
			v, err := toonParse(dec)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return arr, nil
	}
	return nil, fmt.Errorf("unexpected token %v", d)
}

type toonEncoder struct {
	delim, marker string
	lines         []string
}

func toonIndent(depth int) string { return strings.Repeat("  ", depth) }

// fields emits the entries of obj at depth. When hyphen is not empty, the
// first entry is written on that line (a list item's hyphen line) instead of
// being indented; the other entries stay at depth.
func (e *toonEncoder) fields(obj *toonObject, depth int, hyphen string) {
	for i, k := range obj.keys {
		prefix := toonIndent(depth)
		if i == 0 && hyphen != "" {
			prefix = hyphen
		}
		e.field(prefix, toonKey(k), obj.vals[i], depth)
	}
}

// field writes one "key: value" entry whose line starts with prefix. depth is
// the entry's depth; nested content goes to depth+1.
func (e *toonEncoder) field(prefix, key string, v any, depth int) {
	switch x := v.(type) {
	case *toonObject:
		e.lines = append(e.lines, prefix+key+":")
		e.fields(x, depth+1, "")
	case []any:
		e.array(prefix, key, x, depth, true)
	default:
		e.lines = append(e.lines, prefix+key+": "+e.prim(v))
	}
}

// array writes an array header (with key, possibly empty) on prefix and its
// rows or list items. Items and rows live at depth+1. Tabular form is allowed
// everywhere except an array that is itself a list item (spec 9.4).
func (e *toonEncoder) array(prefix, key string, arr []any, depth int, tabular bool) {
	if len(arr) == 0 {
		if key == "" && prefix != "" {
			// keyless list-item array: spec 9.2 forbids "- []"
			e.lines = append(e.lines, prefix+"[0"+e.marker+"]:")
		} else if key == "" {
			e.lines = append(e.lines, prefix+"[]")
		} else {
			e.lines = append(e.lines, prefix+key+": []")
		}
		return
	}
	head := prefix + key + "[" + strconv.Itoa(len(arr)) + e.marker + "]"
	if allPrimitive(arr) {
		cells := make([]string, len(arr))
		for i, v := range arr {
			cells[i] = e.prim(v)
		}
		e.lines = append(e.lines, head+": "+strings.Join(cells, e.delim))
		return
	}
	if fields, rows, ok := toonTabular(arr); ok && tabular {
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = toonKey(f)
		}
		e.lines = append(e.lines, head+"{"+strings.Join(names, e.delim)+"}:")
		for _, row := range rows {
			cells := make([]string, len(row))
			for i, v := range row {
				cells[i] = e.prim(v)
			}
			e.lines = append(e.lines, toonIndent(depth+1)+strings.Join(cells, e.delim))
		}
		return
	}
	e.lines = append(e.lines, head+":")
	for _, v := range arr {
		e.item(depth+1, v)
	}
}

// item writes one list item at depth d: "- " on the hyphen line, object
// fields after the first at d+1, and nested array content at d+2.
func (e *toonEncoder) item(d int, v any) {
	hyphen := toonIndent(d) + "- "
	switch x := v.(type) {
	case *toonObject:
		if len(x.keys) == 0 {
			e.lines = append(e.lines, toonIndent(d)+"-")
			return
		}
		e.fields(x, d+1, hyphen)
	case []any:
		// the hyphen line stands at d; its items go to d+1 (spec 10)
		e.array(hyphen, "", x, d, false)
	default:
		e.lines = append(e.lines, hyphen+e.prim(v))
	}
}

func (e *toonEncoder) prim(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return toonValue(x, e.delim)
	case json.Number:
		return toonNumber(x.String())
	}
	return "null"
}

func allPrimitive(arr []any) bool {
	for _, v := range arr {
		switch v.(type) {
		case *toonObject, []any:
			return false
		}
	}
	return true
}

// toonTabular reports whether arr is uniform: every element is a non-empty
// object with the same key set and primitive values only. It returns the
// header fields (first object's order) and the rows in that order.
func toonTabular(arr []any) ([]string, [][]any, bool) {
	first, ok := arr[0].(*toonObject)
	if !ok || len(first.keys) == 0 {
		return nil, nil, false
	}
	fields := first.keys
	rows := make([][]any, 0, len(arr))
	for _, el := range arr {
		obj, ok := el.(*toonObject)
		if !ok || len(obj.keys) != len(fields) {
			return nil, nil, false
		}
		byKey := make(map[string]any, len(obj.keys))
		for i, k := range obj.keys {
			byKey[k] = obj.vals[i]
		}
		row := make([]any, len(fields))
		for i, f := range fields {
			v, present := byKey[f]
			if !present {
				return nil, nil, false
			}
			switch v.(type) {
			case *toonObject, []any:
				return nil, nil, false
			}
			row[i] = v
		}
		rows = append(rows, row)
	}
	return fields, rows, true
}

// toonKey returns a key unquoted when it matches the plain key grammar.
func toonKey(k string) string {
	if toonKeyPlain.MatchString(k) {
		return k
	}
	return toonEscapeQuoted(k)
}

// toonValue quotes a string value when the spec requires it for delim.
func toonValue(s, delim string) string {
	if !toonNeedsQuote(s, delim) {
		return s
	}
	return toonEscapeQuoted(s)
}

func toonNeedsQuote(s, delim string) bool {
	if s == "" || s == "true" || s == "false" || s == "null" {
		return true
	}
	if s[0] == ' ' || s[0] == '\t' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t' {
		return true
	}
	if s[0] == '-' || s[0] == '#' {
		return true
	}
	if toonNumericLik.MatchString(s) {
		return true
	}
	if strings.ContainsAny(s, ":\"\\[]{}") || strings.Contains(s, delim) {
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 {
			return true
		}
	}
	return false
}

// toonEscapeQuoted wraps s in quotes, escaping per spec 7.1.
func toonEscapeQuoted(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// toonNumber renders a JSON number literal in canonical TOON form. Integer
// literals keep their digits exactly (only -0 becomes 0); other values use the
// shortest round-trip decimal, or exponent form outside [1e-6, 1e21).
func toonNumber(lit string) string {
	if !strings.ContainsAny(lit, ".eE") {
		if lit == "-0" {
			return "0"
		}
		return lit
	}
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return "null"
	}
	switch {
	case math.IsInf(f, 0) || math.IsNaN(f):
		return "null"
	case f == 0:
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // e.g. 1e-07, 1.5e+300
	mant, exp, _ := strings.Cut(s, "e")
	n, _ := strconv.Atoi(exp)
	return fmt.Sprintf("%se%+d", mant, n)
}
