package ciam

import (
	"path/filepath"
	"strings"
)

// Compress reduces source to its structural skeleton: docstrings, block
// comments, comment-only lines and blank runs are dropped, signatures and
// code stay. ext selects the comment syntax (".py", ".go", ".js"...); with an
// empty ext both styles are handled.
func Compress(content, ext string) string {
	hash := ext == "" || ext == ".py" || ext == ".sh" || ext == ".rb" || ext == ".yaml" || ext == ".yml" || ext == ".toml"
	slash := ext == "" || !hash || ext == ".go" || ext == ".js" || ext == ".ts"
	var kept []string
	inDoc, inBlock := false, false
	docDelim := `"""`
	blank := false
	for _, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		if hash {
			if inDoc {
				if strings.Contains(t, docDelim) {
					inDoc = false
				}
				continue
			}
			if strings.HasPrefix(t, `"""`) || strings.HasPrefix(t, `'''`) {
				docDelim = t[:3]
				// a docstring that closes on its own line is a single line
				if !strings.Contains(t[3:], docDelim) {
					inDoc = true
				}
				continue
			}
			if strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "#!") {
				continue
			}
		}
		if slash {
			if inBlock {
				if strings.Contains(t, "*/") {
					inBlock = false
				}
				continue
			}
			if strings.HasPrefix(t, "/*") {
				inBlock = !strings.Contains(t, "*/")
				continue
			}
			if strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "//go:") {
				continue
			}
		}
		if t == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func extOf(path string) string { return strings.ToLower(filepath.Ext(path)) }
