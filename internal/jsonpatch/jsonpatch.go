// Package jsonpatch implements JSON Pointer (RFC 6901) and JSON Patch
// (RFC 6902: add, remove, replace, move, copy, test) over decoded JSON
// values (map[string]any, []any and scalars). Stdlib only.
//
// Apply is atomic: it works on a deep copy of the document, so on any error
// the caller's document is untouched.
//
// ponytail: Apply deep-copies the whole document per call (O(doc size)).
// Fine for memory-sized documents; copy-on-write paths would be the upgrade
// if documents grow to megabytes.
package jsonpatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ParsePointer splits an RFC 6901 pointer into reference tokens. The empty
// pointer refers to the whole document and yields no tokens.
func ParsePointer(ptr string) ([]string, error) {
	if ptr == "" {
		return nil, nil
	}
	if ptr[0] != '/' {
		return nil, fmt.Errorf("pointer %q must be empty or start with '/'", ptr)
	}
	toks := strings.Split(ptr[1:], "/")
	for i, t := range toks {
		// "~1" first, then "~0", as RFC 6901 section 4 requires.
		if strings.Contains(t, "~") {
			var b strings.Builder
			for j := 0; j < len(t); j++ {
				if t[j] != '~' {
					b.WriteByte(t[j])
					continue
				}
				if j+1 >= len(t) || (t[j+1] != '0' && t[j+1] != '1') {
					return nil, fmt.Errorf("pointer %q: invalid escape in token %q", ptr, t)
				}
				if t[j+1] == '0' {
					b.WriteByte('~')
				} else {
					b.WriteByte('/')
				}
				j++
			}
			toks[i] = b.String()
		}
	}
	return toks, nil
}

// Get returns the value at ptr. The result aliases doc.
func Get(doc any, ptr string) (any, error) {
	toks, err := ParsePointer(ptr)
	if err != nil {
		return nil, err
	}
	cur := doc
	for _, t := range toks {
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[t]
			if !ok {
				return nil, fmt.Errorf("%q: no member %q", ptr, t)
			}
			cur = v
		case []any:
			i, err := index(t, len(c))
			if err != nil {
				return nil, fmt.Errorf("%q: %w", ptr, err)
			}
			cur = c[i]
		default:
			return nil, fmt.Errorf("%q: cannot descend into a scalar", ptr)
		}
	}
	return cur, nil
}

// Apply runs patch (a JSON array of operation objects) against doc and
// returns the new document. doc itself is never modified.
func Apply(doc any, patch []any) (any, error) {
	cur := clone(doc)
	for i, raw := range patch {
		op, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("jsonpatch: op %d: not an object", i)
		}
		next, err := applyOp(cur, op)
		if err != nil {
			return nil, fmt.Errorf("jsonpatch: op %d: %w", i, err)
		}
		cur = next
	}
	return cur, nil
}

func applyOp(doc any, op map[string]any) (any, error) {
	name, _ := op["op"].(string)
	path, ok := op["path"].(string)
	if !ok {
		return nil, errors.New(`"path" must be a string`)
	}
	switch name {
	case "add", "replace":
		v, ok := op["value"]
		if !ok {
			return nil, fmt.Errorf("%s: \"value\" is required", name)
		}
		return set(doc, path, clone(v), name == "add")
	case "remove":
		return remove(doc, path)
	case "move", "copy":
		from, ok := op["from"].(string)
		if !ok {
			return nil, fmt.Errorf("%s: \"from\" must be a string", name)
		}
		v, err := Get(doc, from)
		if err != nil {
			return nil, err
		}
		v = clone(v)
		if name == "move" {
			if strings.HasPrefix(path, from+"/") {
				return nil, fmt.Errorf("move: %q cannot be moved into one of its children", from)
			}
			if doc, err = remove(doc, from); err != nil {
				return nil, err
			}
		}
		return set(doc, path, v, true)
	case "test":
		v, ok := op["value"]
		if !ok {
			return nil, errors.New(`test: "value" is required`)
		}
		got, err := Get(doc, path)
		if err != nil {
			return nil, err
		}
		if !Equal(got, v) {
			return nil, fmt.Errorf("test: value at %q does not match", path)
		}
		return doc, nil
	}
	return nil, fmt.Errorf("unknown operation %q", name)
}

// set implements add (insert=true: arrays insert, objects set) and replace
// (insert=false: the target must already exist).
func set(doc any, ptr string, v any, insert bool) (any, error) {
	toks, err := ParsePointer(ptr)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return v, nil
	}
	return update(doc, toks, func(c any, k string) (any, error) {
		switch c := c.(type) {
		case map[string]any:
			if _, exists := c[k]; !exists && !insert {
				return nil, fmt.Errorf("replace: no member %q", k)
			}
			c[k] = v
			return c, nil
		case []any:
			if k == "-" && insert {
				return append(c, v), nil
			}
			if insert {
				i, err := index(k, len(c)+1)
				if err != nil {
					return nil, err
				}
				return slices.Insert(c, i, v), nil
			}
			i, err := index(k, len(c))
			if err != nil {
				return nil, err
			}
			c[i] = v
			return c, nil
		}
		return nil, errors.New("target parent is a scalar")
	})
}

func remove(doc any, ptr string) (any, error) {
	toks, err := ParsePointer(ptr)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errors.New("remove: cannot remove the whole document")
	}
	return update(doc, toks, func(c any, k string) (any, error) {
		switch c := c.(type) {
		case map[string]any:
			if _, ok := c[k]; !ok {
				return nil, fmt.Errorf("remove: no member %q", k)
			}
			delete(c, k)
			return c, nil
		case []any:
			i, err := index(k, len(c))
			if err != nil {
				return nil, err
			}
			return slices.Delete(c, i, i+1), nil
		}
		return nil, errors.New("target parent is a scalar")
	})
}

// update walks toks down from node and calls fn on the last container. fn
// returns the (possibly re-allocated) container, which is stored back into
// its parent, so slices that grow or shrink are handled.
func update(node any, toks []string, fn func(c any, k string) (any, error)) (any, error) {
	if len(toks) == 1 {
		return fn(node, toks[0])
	}
	switch n := node.(type) {
	case map[string]any:
		child, ok := n[toks[0]]
		if !ok {
			return nil, fmt.Errorf("no member %q", toks[0])
		}
		nc, err := update(child, toks[1:], fn)
		if err != nil {
			return nil, err
		}
		n[toks[0]] = nc
		return n, nil
	case []any:
		i, err := index(toks[0], len(n))
		if err != nil {
			return nil, err
		}
		nc, err := update(n[i], toks[1:], fn)
		if err != nil {
			return nil, err
		}
		n[i] = nc
		return n, nil
	}
	return nil, errors.New("cannot descend into a scalar")
}

// index parses an RFC 6901 array index (no sign, no leading zeros, "-" is
// rejected here) and checks it is below limit.
func index(tok string, limit int) (int, error) {
	if tok == "" || (len(tok) > 1 && tok[0] == '0') {
		return 0, fmt.Errorf("invalid array index %q", tok)
	}
	for _, c := range tok {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid array index %q", tok)
		}
	}
	i, err := strconv.Atoi(tok)
	if err != nil || i >= limit {
		return 0, fmt.Errorf("array index %q out of range", tok)
	}
	return i, nil
}

// clone deep-copies maps and slices; scalars are shared (they are immutable).
func clone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = clone(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = clone(e)
		}
		return out
	}
	return v
}

// Equal compares two JSON values. Numbers compare by value whatever their Go
// type (float64, int, json.Number...).
func Equal(a, b any) bool {
	if fa, ok := number(a); ok {
		fb, ok := number(b)
		return ok && fa == fb
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, e := range x {
			f, ok := y[k]
			if !ok || !Equal(e, f) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}
