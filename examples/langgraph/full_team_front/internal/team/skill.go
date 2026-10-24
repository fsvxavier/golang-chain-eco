package team

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/examples/langgraph/full_team_front/skills"
)

// skillDir maps a role to its folder under skills/.
var skillDir = map[string]string{
	"manager": "engineering-manager", "po": "product-owner", "leader": "tech-leader",
	"junior": "software-engineer-junior", "mid": "software-engineer-mid", "senior": "software-engineer-senior",
	"specialist": "specialist-reviewer", "qa": "qa-engineer",
}

// body drops a file's frontmatter and surrounding whitespace.
func body(s string) string {
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		if _, b, ok := strings.Cut(rest, "\n---\n"); ok {
			return strings.TrimSpace(b)
		}
	}
	return s
}

// Skill returns the body of the role's SKILL.md without its frontmatter (the
// agent's system prompt), followed by each vendored reference of the role,
// sorted by name, under a "## Skill: <name>" heading.
func Skill(role string) (string, error) {
	dir, ok := skillDir[role]
	if !ok {
		return "", fmt.Errorf("unknown role %q", role)
	}
	b, err := skills.FS.ReadFile(dir + "/SKILL.md")
	if err != nil {
		return "", err
	}
	out := body(string(b))
	refs, err := skills.FS.ReadDir(dir + "/references")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	for _, r := range refs { // ReadDir sorts by name
		rb, err := skills.FS.ReadFile(dir + "/references/" + r.Name() + "/SKILL.md")
		if err != nil {
			return "", err
		}
		out += "\n\n## Skill: " + r.Name() + "\n\n" + body(string(rb))
	}
	return out, nil
}
