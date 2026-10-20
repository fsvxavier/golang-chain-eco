// Package kb embeds the documents the knowledge services index.
package kb

import "embed"

//go:embed *.md
var FS embed.FS
