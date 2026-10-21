package compress

import (
	"encoding/json"
	"testing"
)

func toonGolden(t *testing.T, in, want string) {
	t.Helper()
	got, err := TOONFromJSON(in, TOONOptions{})
	if err != nil {
		t.Fatalf("TOONFromJSON(%s): unexpected error %v", in, err)
	}
	if got != want {
		t.Fatalf("TOONFromJSON(%s)\n got: %q\nwant: %q", in, got, want)
	}
}

func TestTOONTabularSpecExample(t *testing.T) {
	toonGolden(t,
		`{"users":[{"id":1,"name":"Ada","role":"admin"},{"id":2,"name":"Bob","role":"user"}]}`,
		"users[2]{id,name,role}:\n  1,Ada,admin\n  2,Bob,user")
}

func TestTOONPrimitiveArray(t *testing.T) {
	toonGolden(t, `{"tags":["admin","ops","dev"]}`, "tags[3]: admin,ops,dev")
	toonGolden(t, `{"a":[]}`, "a: []")
	toonGolden(t, `[]`, "[]")
	toonGolden(t, `[1,2,3]`, "[3]: 1,2,3")
	toonGolden(t, `[1,2]`, "[2]: 1,2")
}

func TestTOONNestedObjects(t *testing.T) {
	toonGolden(t, `{"a":{"b":{"c":1}}}`, "a:\n  b:\n    c: 1")
	toonGolden(t, `{"user":{"name":"Ada","tags":["x","y"]}}`, "user:\n  name: Ada\n  tags[2]: x,y")
	toonGolden(t, `{"a":{}}`, "a:")
	toonGolden(t, `{}`, "")
}

func TestTOONKeyOrderPreserved(t *testing.T) {
	toonGolden(t, `{"z":1,"a":2,"m":3}`, "z: 1\na: 2\nm: 3")
}

func TestTOONMixedArrayListForm(t *testing.T) {
	toonGolden(t, `{"m":[1,{"a":1},"x"]}`, "m[3]:\n  - 1\n  - a: 1\n  - x")
	toonGolden(t, `{"m":[{"id":1,"meta":{"k":"v"},"n":2},1]}`,
		"m[2]:\n  - id: 1\n    meta:\n      k: v\n    n: 2\n  - 1")
	toonGolden(t, `{"m":[{},1]}`, "m[2]:\n  -\n  - 1")
	toonGolden(t, `{"m":[[1,2],"x"]}`, "m[2]:\n  - [2]: 1,2\n  - x")
	toonGolden(t, `{"m":[[{"a":1}],2]}`, "m[2]:\n  - [1]:\n      - a: 1\n  - 2")
	toonGolden(t, `{"m":[[],"x"]}`, "m[2]:\n  - []\n  - x")
	toonGolden(t, `{"m":[{"users":[{"a":1},{"a":2}],"z":1},3]}`,
		"m[2]:\n  - users[2]{a}:\n      1\n      2\n    z: 1\n  - 3")
}

func TestTOONNonUniformObjectsListForm(t *testing.T) {
	toonGolden(t, `{"list":[{"a":1},{"b":2}]}`, "list[2]:\n  - a: 1\n  - b: 2")
	toonGolden(t, `{"list":[{"a":1},{}]}`, "list[2]:\n  - a: 1\n  -")
	toonGolden(t, `{"list":[{"a":{"b":1}},{"a":{"b":2}}]}`, "list[2]:\n  - a:\n      b: 1\n  - a:\n      b: 2")
}

func TestTOONTabularNullsAndReorderedKeys(t *testing.T) {
	toonGolden(t, `{"list":[{"a":null},{"a":"x"}]}`, "list[2]{a}:\n  null\n  x")
	toonGolden(t, `{"l":[{"a":1,"b":2},{"b":3,"a":4}]}`, "l[2]{a,b}:\n  1,2\n  4,3")
}

func TestTOONStringQuoting(t *testing.T) {
	cases := map[string]string{
		"true":          `"true"`,
		"false":         `"false"`,
		"null":          `"null"`,
		"42":            `"42"`,
		"05":            `"05"`,
		"1e-6":          `"1e-6"`,
		"+1":            `"+1"`,
		"a,b":           `"a,b"`,
		"x: y":          `"x: y"`,
		" lead":         `" lead"`,
		"trail ":        `"trail "`,
		"":              `""`,
		"-x":            `"-x"`,
		"-":             `"-"`,
		"#x":            `"#x"`,
		"line\nbreak":   `"line\nbreak"`,
		`say "hi"`:      `"say \"hi\""`,
		`back\slash`:    `"back\\slash"`,
		"[x]":           `"[x]"`,
		"{x}":           `"{x}"`,
		"a\tb":          `"a\tb"`,
		"x\x01":         `"x\u0001"`,
		"a b":           "a b",
		"Ada Lovelace":  "Ada Lovelace",
		"a-b":           "a-b",
		"héllo 世界 🚀":    "héllo 世界 🚀",
		"😀 ok":          "😀 ok",
		"2024-01-01T00": "2024-01-01T00",
	}
	for in, want := range cases {
		raw, err := json.Marshal(map[string]string{"v": in})
		if err != nil {
			t.Fatal(err)
		}
		toonGolden(t, string(raw), "v: "+want)
	}
}

func TestTOONKeyQuoting(t *testing.T) {
	toonGolden(t, `{"my-key":1}`, `"my-key": 1`)
	toonGolden(t, `{"a.b_c":1}`, "a.b_c: 1")
	toonGolden(t, `{"1x":1}`, `"1x": 1`)
	toonGolden(t, `{"":1}`, `"": 1`)
	toonGolden(t, `{"名前":"x"}`, `"名前": x`)
	toonGolden(t, `{"list":[{"my-key":1,"b":2},{"my-key":3,"b":4}]}`,
		"list[2]{\"my-key\",b}:\n  1,2\n  3,4")
	toonGolden(t, `{"my-list":[1]}`, `"my-list"[1]: 1`)
}

func TestTOONLiterals(t *testing.T) {
	toonGolden(t, `{"a":null,"b":true,"c":false}`, "a: null\nb: true\nc: false")
}

func TestTOONNumbers(t *testing.T) {
	cases := map[string]string{
		`1.50`:                 "1.5",
		`1.0`:                  "1",
		`-0`:                   "0",
		`-0.0`:                 "0",
		`0e1`:                  "0",
		`-1E+03`:               "-1000",
		`1e6`:                  "1000000",
		`1e21`:                 "1e+21",
		`1.5e300`:              "1.5e+300",
		`1e-7`:                 "1e-7",
		`0.000001`:             "0.000001",
		`0.25`:                 "0.25",
		`12345678901234567890`: "12345678901234567890",
		`-42`:                  "-42",
	}
	for in, want := range cases {
		toonGolden(t, `{"v":`+in+`}`, "v: "+want)
	}
}

func TestTOONUnicodeKeysAndValues(t *testing.T) {
	toonGolden(t, `{"emoji":"héllo 世界 🚀"}`, "emoji: héllo 世界 🚀")
}

func TestTOONDelimiterPipe(t *testing.T) {
	got, err := TOONFromJSON(
		`{"users":[{"id":1,"name":"Ada, L","role":"admin"},{"id":2,"name":"Bob","role":"user"}]}`,
		TOONOptions{Delimiter: "|"})
	if err != nil {
		t.Fatal(err)
	}
	want := "users[2|]{id|name|role}:\n  1|Ada, L|admin\n  2|Bob|user"
	if got != want {
		t.Fatalf("pipe:\n got: %q\nwant: %q", got, want)
	}
	got, err = TOONFromJSON(`{"t":["a|b","c"]}`, TOONOptions{Delimiter: "|"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `t[2|]: "a|b"|c`; got != want {
		t.Fatalf("pipe primitive:\n got: %q\nwant: %q", got, want)
	}
}

func TestTOONDelimiterTab(t *testing.T) {
	got, err := TOONFromJSON(`{"t":["a","b"]}`, TOONOptions{Delimiter: "\t"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "t[2\t]: a\tb"; got != want {
		t.Fatalf("tab primitive:\n got: %q\nwant: %q", got, want)
	}
	got, err = TOONFromJSON(`{"t":["a,b","c"]}`, TOONOptions{Delimiter: "\t"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "t[2\t]: a,b\tc"; got != want {
		t.Fatalf("tab comma:\n got: %q\nwant: %q", got, want)
	}
	got, err = TOONFromJSON(`{"l":[{"a":1,"b":"x y"},{"a":2,"b":"z"}]}`, TOONOptions{Delimiter: "\t"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "l[2\t]{a\tb}:\n  1\tx y\n  2\tz"; got != want {
		t.Fatalf("tab tabular:\n got: %q\nwant: %q", got, want)
	}
}

func TestTOONDelimiterCommaExplicit(t *testing.T) {
	got, err := TOONFromJSON(`{"t":["a","b"]}`, TOONOptions{Delimiter: ","})
	if err != nil {
		t.Fatal(err)
	}
	if want := "t[2]: a,b"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTOONInvalidInput(t *testing.T) {
	bad := []string{``, `{"a":`, `{"a":1} x`, `{"a":1}{"b":2}`, `{a:1}`, `[1,]`}
	for _, in := range bad {
		if _, err := TOONFromJSON(in, TOONOptions{}); err == nil {
			t.Fatalf("TOONFromJSON(%q): want error", in)
		}
	}
	if _, err := TOONFromJSON(`{}`, TOONOptions{Delimiter: ";"}); err == nil {
		t.Fatal("unsupported delimiter: want error")
	}
}

func TestTOONShorterNeverLonger(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{`{"a":1}`, "a: 1", true},
		{`{"a":"b"}`, "a: b", true},
		{`{"a":{"b":1}}`, "a:\n  b: 1", true},
		{`{"x":[1]}`, "x[1]: 1", true},
		{`{"a":[]}`, "a: []", true},
		{`[]`, "", false},
		{`[1,2]`, "", false},
		{`[1]`, "", false},
		{`[{"id":1}]`, "", false},
		{`"str"`, "", false},
		{`42`, "", false},
		{`not json`, "", false},
		{`{"a":`, "", false},
	}
	for _, c := range cases {
		got, ok := TOONShorter(c.in)
		if ok != c.wantOK {
			t.Fatalf("TOONShorter(%q) ok=%v want %v", c.in, ok, c.wantOK)
		}
		if ok {
			if c.want != "" && got != c.want {
				t.Fatalf("TOONShorter(%q) = %q want %q", c.in, got, c.want)
			}
			if len(got) >= len(c.in) {
				t.Fatalf("TOONShorter(%q) returned non-shorter %q", c.in, got)
			}
		} else if got != c.in {
			t.Fatalf("TOONShorter(%q) changed input to %q", c.in, got)
		}
	}
}

func TestTOONShorterEmptyOutputStaysJSON(t *testing.T) {
	// a root empty object encodes to the empty document, which is not a rewrite
	for _, in := range []string{`{}`, ` {} `} {
		got, ok := TOONShorter(in)
		if ok || got != in {
			t.Fatalf("TOONShorter(%q) = %q, %v; want %q, false", in, got, ok, in)
		}
	}
}
