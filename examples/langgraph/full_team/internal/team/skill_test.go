package team

import (
	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/skills"
	"strings"
	"testing"
)

func TestSkillIncludesReferences(t *testing.T) {
	cases := map[string][]string{
		"leader":     {"## Skill: code-review", "## Skill: planning-and-task-breakdown"},
		"po":         {"## Skill: contract-first", "## Skill: user-stories"},
		"manager":    {"## Skill: planning-and-task-breakdown"},
		"specialist": {"## Skill: performance-optimization"},
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
	got, _ := Skill("leader")
	if a, b := strings.Index(got, "## Skill: code-review"), strings.Index(got, "## Skill: planning-and-task-breakdown"); a < 0 || a > b {
		t.Error("references are not sorted by name")
	}
}

func TestSkillUnknownRole(t *testing.T) {
	if _, err := Skill("nobody"); err == nil || !strings.Contains(err.Error(), `unknown role "nobody"`) {
		t.Fatalf("err = %v", err)
	}
}

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
}

// The manager's reply is rejected when it contains a code fence, so its
// vendored references must not model one (its own persona shows the reply
// shape in fences, which is why only the references are checked).
func TestManagerReferencesHaveNoFences(t *testing.T) {
	for _, r := range skillRefs("manager") {
		raw, err := skills.FS.ReadFile(skillDir["manager"] + "/references/" + r + "/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "```") {
			t.Errorf("manager reference %s contains a code fence", r)
		}
	}
}
