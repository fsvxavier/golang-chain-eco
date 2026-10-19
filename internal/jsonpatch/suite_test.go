package jsonpatch

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestOfficialSuite runs the json-patch/json-patch-tests suite (testdata/).
// A case with "error" must fail; one with "expected" must produce it; one
// with neither must just succeed. "disabled" cases are skipped, as the suite
// asks.
func TestOfficialSuite(t *testing.T) {
	for _, file := range []string{"spec_tests.json", "tests.json"} {
		b, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var cases []map[string]any
		if err := json.Unmarshal(b, &cases); err != nil {
			t.Fatal(err)
		}
		for i, c := range cases {
			name := fmt.Sprintf("%s#%d %v", file, i, c["comment"])
			if c["disabled"] == true {
				continue
			}
			patch, _ := c["patch"].([]any)
			got, err := Apply(c["doc"], patch)
			if _, wantErr := c["error"]; wantErr {
				if err == nil {
					t.Errorf("%s: want error %q, got %v", name, c["error"], got)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			if want, ok := c["expected"]; ok && !Equal(got, want) {
				t.Errorf("%s: got %v, want %v", name, got, want)
			}
		}
	}
}
