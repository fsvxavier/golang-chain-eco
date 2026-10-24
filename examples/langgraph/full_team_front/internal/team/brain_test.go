package team

import (
	"strings"
	"testing"
)

// The offline scenario is a front-end screen: no schema, SQL, Go or rate limit.
func TestBrainScenarioIsFrontEnd(t *testing.T) {
	spec := Brain("ROLE: po\n## Backend named by the demand\nROOT: /x\n")
	plan := Brain("ROLE: leader\n")
	code := Brain("ROLE: engineer\nTASK: T1\nATTEMPT: 1\nWHO: junior-1\n")
	for what, text := range map[string]string{"spec": spec, "plan": plan, "code": code,
		"leader review": Brain("ROLE: leader\nMODE: review\nTASK: T2\nATTEMPT: 1\n"),
		"specialist":    Brain("ROLE: specialist\nTASK: T3\nATTEMPT: 1\n"),
		"qa":            Brain("ROLE: qa\nTASK: T4\nATTEMPT: 1\n")} {
		for _, bad := range []string{"table", "schema", "```go", "rate limit", "TTL", "422", "endpoint"} {
			if strings.Contains(strings.ToLower(text), strings.ToLower(bad)) {
				t.Errorf("%s still mentions %q:\n%s", what, bad, text)
			}
		}
	}
	for _, want := range []string{"aria-live", "UI_STATES:", "AC4:"} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec lacks %q", want)
		}
	}
	if !strings.Contains(code, "```tsx") {
		t.Errorf("engineer output is not TSX:\n%s", code)
	}
	if ts := parsePlan(plan); len(ts) != 5 || ts[2].Prefer != "senior-1" || ts[2].Complexity != "high" {
		t.Errorf("plan shape changed: %+v", ts)
	}
}
