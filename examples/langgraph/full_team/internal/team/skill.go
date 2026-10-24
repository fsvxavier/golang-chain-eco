package team

import (
	"fmt"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team/skills"
)

// skillDir maps a role to its folder under skills/.
var skillDir = map[string]string{
	"manager": "engineering-manager", "po": "product-owner", "architect": "architect", "leader": "tech-leader",
	"junior": "software-engineer-junior", "mid": "software-engineer-mid", "senior": "software-engineer-senior",
	"specialist": "specialist-reviewer", "qa": "qa-engineer",
}

// Skill returns the body of the role's SKILL.md without its frontmatter: the
// agent's system prompt.
func Skill(role string) (string, error) {
	dir, ok := skillDir[role]
	if !ok {
		return "", fmt.Errorf("unknown role %q", role)
	}
	b, err := skills.FS.ReadFile(dir + "/SKILL.md")
	if err != nil {
		return "", err
	}
	s := string(b)
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			return strings.TrimSpace(body), nil
		}
	}
	return s, nil
}

// skillRefs lists the names of the reference skills vendored into the role's skill.
func skillRefs(role string) []string {
	refs, _ := skills.FS.ReadDir(skillDir[role] + "/references")
	var out []string
	for _, r := range refs {
		out = append(out, r.Name())
	}
	return out
}
