// Package skills embeds the persona of every agent and the vendored skills
// each persona carries under references/ (pinned in skills.lock).
package skills

import "embed"

//go:embed */SKILL.md */references/*/SKILL.md skills.lock THIRD_PARTY_NOTICES.md
var FS embed.FS
