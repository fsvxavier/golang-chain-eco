package team

import (
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/skills"
	"regexp"
	"strings"
	"testing"
)

func TestSkillIncludesReferences(t *testing.T) {
	cases := map[string][]string{
		"qa":     {"## Skill: playwright-best-practices", "## Skill: webapp-testing"},
		"junior": {"## Skill: vercel-react-best-practices", "## Skill: react-testing"},
		"leader": {"## Skill: code-review", "## Skill: planning-and-task-breakdown"},
		"po":     {"## Skill: contract-first", "## Skill: user-stories"},
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
	if a, b := strings.Index(got, "## Skill: frontend-patterns"), strings.Index(got, "## Skill: react-testing"); a < 0 || a > b {
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

// Every vendored reference shows up as its own section, with its frontmatter
// stripped, for every role.
func TestSkillSectionsHaveNoFrontmatter(t *testing.T) {
	for role, dir := range skillDir {
		got, err := Skill(role)
		if err != nil {
			t.Fatal(err)
		}
		refs, _ := skills.FS.ReadDir(dir + "/references")
		for _, r := range refs {
			raw, err := skills.FS.ReadFile(dir + "/references/" + r.Name() + "/SKILL.md")
			if err != nil {
				t.Fatal(err)
			}
			want := "## Skill: " + r.Name() + "\n\n" + body(string(raw))
			if !strings.Contains(got, want) {
				t.Errorf("Skill(%q) lacks section %q with its body", role, r.Name())
			}
			if strings.Contains(got, "## Skill: "+r.Name()+"\n\n---") || strings.Contains(got, "\n---\nname: "+r.Name()) {
				t.Errorf("Skill(%q) section %q keeps its frontmatter", role, r.Name())
			}
		}
	}
	got, _ := Skill("specialist")
	for _, n := range []string{"accessibility", "frontend-code-review", "performance-optimization"} {
		if !strings.Contains(got, "## Skill: "+n+"\n") {
			t.Errorf("specialist lacks %s", n)
		}
	}
}

// The manager's reply is rejected when it contains a code fence, so its persona
// and references must not model one.
func TestManagerSkillHasNoFences(t *testing.T) {
	got, _ := Skill("manager")
	if strings.Contains(got, "```") {
		t.Error("manager skill contains a code fence")
	}
}
