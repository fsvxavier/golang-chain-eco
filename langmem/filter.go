package langmem

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

// Filter operators for IndexedStore.Search and SemanticSearch. A filter value
// that is a map whose keys start with "$" is an operator expression; any other
// value is plain equality, as in store.Store:
//
//	{"kind": "fact", "priority": {"$gte": 2}, "tag": {"$in": []any{"a", "b"}}}
//
// $eq $ne $gt $gte $lt $lte $in. Ordering operators compare numbers or strings
// and match nothing for other operands. A missing field matches $ne only.
var filterOps = map[string]bool{"$eq": true, "$ne": true, "$gt": true, "$gte": true, "$lt": true, "$lte": true, "$in": true}

// splitFilter separates plain-equality entries, which the underlying store
// evaluates, from operator entries, which IndexedStore evaluates itself.
func splitFilter(f map[string]any) (eq map[string]any, ops map[string]map[string]any, err error) {
	eq, ops = map[string]any{}, map[string]map[string]any{}
	for field, cond := range f {
		m, isMap := cond.(map[string]any)
		if !isMap || !hasOp(m) {
			eq[field] = cond
			continue
		}
		for op := range m {
			if !filterOps[op] {
				return nil, nil, fmt.Errorf("langmem: unknown filter operator %q on %q", op, field)
			}
		}
		ops[field] = m
	}
	return eq, ops, nil
}

func hasOp(m map[string]any) bool {
	for k := range m {
		if strings.HasPrefix(k, "$") {
			return true
		}
	}
	return false
}

// matchEq reports whether value satisfies every plain-equality entry.
func matchEq(value, eq map[string]any) bool {
	for field, want := range eq {
		if !equal(value[field], want) {
			return false
		}
	}
	return true
}

// matchOps reports whether value satisfies every operator entry.
func matchOps(value map[string]any, ops map[string]map[string]any) bool {
	for field, conds := range ops {
		v, present := value[field]
		for op, arg := range conds {
			if !testOp(op, v, present, arg) {
				return false
			}
		}
	}
	return true
}

func testOp(op string, v any, present bool, arg any) bool {
	switch op {
	case "$eq":
		return present && equal(v, arg)
	case "$ne":
		return !present || !equal(v, arg)
	case "$in":
		if !present {
			return false
		}
		rv := reflect.ValueOf(arg)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return false
		}
		for i := range rv.Len() {
			if equal(v, rv.Index(i).Interface()) {
				return true
			}
		}
		return false
	}
	c, ok := compare(v, arg)
	if !ok || !present {
		return false
	}
	switch op {
	case "$gt":
		return c > 0
	case "$gte":
		return c >= 0
	case "$lt":
		return c < 0
	}
	return c <= 0 // $lte
}

// equal compares numbers by value (JSON backends decode them as float64) and
// everything else deeply.
func equal(a, b any) bool {
	if x, ok := num(a); ok {
		if y, ok := num(b); ok {
			return x == y
		}
		return false
	}
	return reflect.DeepEqual(a, b)
}

func compare(a, b any) (int, bool) {
	if x, ok := num(a); ok {
		y, ok := num(b)
		if !ok {
			return 0, false
		}
		switch {
		case x < y:
			return -1, true
		case x > y:
			return 1, true
		}
		return 0, true
	}
	x, ok1 := a.(string)
	y, ok2 := b.(string)
	if ok1 && ok2 {
		return strings.Compare(x, y), true
	}
	return 0, false
}

func num(v any) (float64, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}

// filterItems keeps the items whose value satisfies the operators.
func filterItems(items []store.Item, ops map[string]map[string]any) []store.Item {
	if len(ops) == 0 {
		return items
	}
	out := items[:0]
	for _, it := range items {
		if matchOps(it.Value, ops) {
			out = append(out, it)
		}
	}
	return out
}
