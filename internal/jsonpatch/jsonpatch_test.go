package jsonpatch

import (
	"encoding/json"
	"reflect"
	"testing"
)

// mustJSON decodes a literal; the test vectors below are JSON text.
func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad literal %s: %v", s, err)
	}
	return v
}

// The cases follow RFC 6902 Appendix A (section names in comments) and the
// pointer examples of RFC 6901 section 5. Written from the RFC text, not
// downloaded; suite_test.go runs the official json-patch-tests suite too.
func TestApplyRFC6902(t *testing.T) {
	cases := []struct {
		name    string
		doc     string
		patch   string
		want    string // "" means expect an error
		wantErr bool
	}{
		{"A.1 add object member", `{"foo": "bar"}`,
			`[{"op": "add", "path": "/baz", "value": "qux"}]`,
			`{"baz": "qux", "foo": "bar"}`, false},
		{"A.2 add array element", `{"foo": ["bar", "baz"]}`,
			`[{"op": "add", "path": "/foo/1", "value": "qux"}]`,
			`{"foo": ["bar", "qux", "baz"]}`, false},
		{"A.3 remove object member", `{"baz": "qux", "foo": "bar"}`,
			`[{"op": "remove", "path": "/baz"}]`,
			`{"foo": "bar"}`, false},
		{"A.4 remove array element", `{"foo": ["bar", "qux", "baz"]}`,
			`[{"op": "remove", "path": "/foo/1"}]`,
			`{"foo": ["bar", "baz"]}`, false},
		{"A.5 replace value", `{"baz": "qux", "foo": "bar"}`,
			`[{"op": "replace", "path": "/baz", "value": "boo"}]`,
			`{"baz": "boo", "foo": "bar"}`, false},
		{"A.6 move value", `{"foo": {"bar": "baz", "waldo": "fred"}, "qux": {"corge": "grault"}}`,
			`[{"op": "move", "from": "/foo/waldo", "path": "/qux/thud"}]`,
			`{"foo": {"bar": "baz"}, "qux": {"corge": "grault", "thud": "fred"}}`, false},
		{"A.7 move array element", `{"foo": ["all", "grass", "cows", "eat"]}`,
			`[{"op": "move", "from": "/foo/1", "path": "/foo/3"}]`,
			`{"foo": ["all", "cows", "eat", "grass"]}`, false},
		{"A.8 testing success", `{"baz": "qux", "foo": ["a", 2, "c"]}`,
			`[{"op": "test", "path": "/baz", "value": "qux"}, {"op": "test", "path": "/foo/1", "value": 2}]`,
			`{"baz": "qux", "foo": ["a", 2, "c"]}`, false},
		{"A.9 testing error", `{"baz": "qux"}`,
			`[{"op": "test", "path": "/baz", "value": "bar"}]`, "", true},
		{"A.10 nonexistent target location", `{"foo": "bar"}`,
			`[{"op": "add", "path": "/baz/bat", "value": "qux"}]`, "", true},
		{"A.11 ignoring unrecognized elements", `{"foo": "bar"}`,
			`[{"op": "add", "path": "/baz", "value": "qux", "xyz": 123}]`,
			`{"foo": "bar", "baz": "qux"}`, false},
		{"A.12 adding to a nonexistent target", `{"foo": "bar"}`,
			`[{"op": "add", "path": "/baz/bat", "value": "qux"}]`, "", true},
		{"A.13 invalid patch input (add without value)", `{"foo": "bar"}`,
			`[{"op": "add", "path": "/baz"}]`, "", true},
		{"A.14 ~ escape ordering", `{"/": 9, "~1": 10}`,
			`[{"op": "test", "path": "/~01", "value": 10}]`,
			`{"/": 9, "~1": 10}`, false},
		{"A.15 comparing strings and numbers", `{"/": 9, "~1": 10}`,
			`[{"op": "test", "path": "/~01", "value": "10"}]`, "", true},
		{"A.16 adding an array value", `{"foo": ["bar"]}`,
			`[{"op": "add", "path": "/foo/-", "value": ["abc", "def"]}]`,
			`{"foo": ["bar", ["abc", "def"]]}`, false},
		// RFC 6901 section 5 pointer examples, used as targets.
		{"6901 slash and tilde tokens", `{"a/b": {"m~n": 1}}`,
			`[{"op": "replace", "path": "/a~1b/m~0n", "value": 2}]`,
			`{"a/b": {"m~n": 2}}`, false},
		{"6902 root replace", `{"a": 1}`,
			`[{"op": "replace", "path": "", "value": [1, 2]}]`,
			`[1, 2]`, false},
		{"6902 copy", `{"a": {"x": [1]}}`,
			`[{"op": "copy", "from": "/a", "path": "/b"}]`,
			`{"a": {"x": [1]}, "b": {"x": [1]}}`, false},
		{"6902 move into own child is an error", `{"a": {"b": 1}}`,
			`[{"op": "move", "from": "/a", "path": "/a/c"}]`, "", true},
		{"6902 leading zero index is an error", `{"a": [1, 2]}`,
			`[{"op": "remove", "path": "/a/01"}]`, "", true},
		{"6902 dash is not a valid remove index", `{"a": [1, 2]}`,
			`[{"op": "remove", "path": "/a/-"}]`, "", true},
		{"6902 replace missing member is an error", `{"a": 1}`,
			`[{"op": "replace", "path": "/b", "value": 2}]`, "", true},
		{"6902 remove missing member is an error", `{"a": 1}`,
			`[{"op": "remove", "path": "/b"}]`, "", true},
		{"6902 unknown op is an error", `{"a": 1}`,
			`[{"op": "frob", "path": "/a"}]`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := mustJSON(t, c.doc)
			patch := mustJSON(t, c.patch).([]any)
			before := mustJSON(t, c.doc)
			got, err := Apply(doc, patch)
			if !reflect.DeepEqual(doc, before) {
				t.Fatalf("Apply modified its input")
			}
			if c.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if want := mustJSON(t, c.want); !Equal(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestApplyIsAtomic(t *testing.T) {
	doc := mustJSON(t, `{"a": 1, "b": [1, 2]}`)
	patch := mustJSON(t, `[
		{"op": "replace", "path": "/a", "value": 9},
		{"op": "remove", "path": "/b/0"},
		{"op": "test", "path": "/a", "value": 1}
	]`).([]any)
	if _, err := Apply(doc, patch); err == nil {
		t.Fatal("third op must fail")
	}
	if !reflect.DeepEqual(doc, mustJSON(t, `{"a": 1, "b": [1, 2]}`)) {
		t.Fatalf("failed patch changed the document: %v", doc)
	}
}

func TestGetAndPointerErrors(t *testing.T) {
	doc := mustJSON(t, `{"a/b": [10, {"c": true}]}`)
	v, err := Get(doc, "/a~1b/1/c")
	if err != nil || v != true {
		t.Fatalf("got %v %v", v, err)
	}
	for _, bad := range []string{"a", "/a~2", "/a~", "/a/9", "/missing"} {
		if _, err := Get(doc, bad); err == nil {
			t.Errorf("Get(%q): want error", bad)
		}
	}
}

func TestEqualNumbers(t *testing.T) {
	if !Equal(float64(3), 3) || !Equal(int64(3), mustJSON(t, "3")) {
		t.Fatal("numbers must compare by value")
	}
	if Equal("3", float64(3)) {
		t.Fatal("string must not equal number")
	}
}
