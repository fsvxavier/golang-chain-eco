package team

import (
	"regexp"
	"strings"
	"testing"
)

func TestSkillIncludesReferences(t *testing.T) {
	cases := map[string][]string{
		"qa":     {"## Skill: playwright-cli", "## Skill: webapp-testing"},
		"junior": {"## Skill: vercel-react-best-practices", "## Skill: react-testing", "## Skill: ui-ux-pro-max"},
		"leader": {"## Skill: code-review", "## Skill: planning-and-task-breakdown"},
		"po":     {"## Skill: contract-first", "## Skill: openapi-spec-generation"},
	}
	for role, want := range cases {
		got, err := Skill(role)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("Skill(%q) lacks %q", role, w)
			}
		}
		if strings.Contains(got, "name: ") && strings.Contains(got, "\n---\n") {
			t.Errorf("Skill(%q) still carries frontmatter", role)
		}
	}
	// References come after the persona and are sorted by name.
	got, _ := Skill("junior")
	if a, b := strings.Index(got, "## Skill: frontend-design"), strings.Index(got, "## Skill: frontend-patterns"); a < 0 || a > b {
		t.Error("references are not sorted by name")
	}
}

func TestSkillUnknownRole(t *testing.T) {
	if _, err := Skill("nobody"); err == nil || !strings.Contains(err.Error(), `unknown role "nobody"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestNoArchitectInSkills(t *testing.T) {
	for role := range skillDir {
		got, _ := Skill(role)
		if role != "po" && architectRole.MatchString(got) {
			t.Errorf("Skill(%q) mentions an architect", role)
		}
	}
}

var architectRole = regexp.MustCompile(`(?i)\barchitects?\b`)
