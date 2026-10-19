package ciam

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DocKind is a kind of knowledge document under docs/.
type DocKind string

const (
	ADR      DocKind = "adr"      // architecture decision record: the why
	PRD      DocKind = "prd"      // product requirements: what and acceptance criteria
	Plan     DocKind = "plan"     // phased implementation plan
	Research DocKind = "research" // external research
)

func (k DocKind) dir() string {
	if k == Plan {
		return "plans"
	}
	return string(k)
}

func (k DocKind) prefix() string {
	switch k {
	case ADR:
		return "ADR"
	case PRD:
		return "PRD"
	case Plan:
		return "Plan"
	}
	return "RES"
}

// Doc is a parsed knowledge document.
type Doc struct {
	Kind   DocKind `json:"kind"`
	Number int     `json:"number"`
	Title  string  `json:"title"`
	Status string  `json:"status"`
	Path   string  `json:"path"`
}

// Docs creates and lists ADR/PRD/Plan/Research documents in a project's docs/
// directory. They are plain Markdown with YAML front matter, meant to be
// committed with the code and indexed (they get chunk types adr/prd/plan/
// research) so assistants can search the reasoning behind the code.
type Docs struct{ root string }

// NewDocs scopes a Docs manager to a project root.
func NewDocs(projectRoot string) *Docs {
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		abs = projectRoot
	}
	return &Docs{root: abs}
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(title string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-"), "-")
	if s == "" {
		return "untitled"
	}
	return s
}

func (d *Docs) dir(k DocKind) string { return filepath.Join(d.root, "docs", k.dir()) }

var numRe = regexp.MustCompile(`^[A-Za-z]+-(\d+)-`)

func nextNumber(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	max := 0
	for _, e := range entries {
		if m := numRe.FindStringSubmatch(e.Name()); m != nil && strings.HasSuffix(e.Name(), ".md") {
			if n, _ := strconv.Atoi(m[1]); n > max {
				max = n
			}
		}
	}
	return max + 1, nil
}

func render(tmpl, title string, number int, extra map[string]string) string {
	r := strings.NewReplacer("{{DATE}}", time.Now().Format("2006-01-02"), "{{TITLE}}", title, "{{NUMBER}}", fmt.Sprintf("%03d", number))
	out := r.Replace(tmpl)
	for k, v := range extra {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

// New creates a document of the given kind and returns its path. For a Plan,
// prdRef (e.g. "PRD-001") links the PRD it implements.
func (d *Docs) New(kind DocKind, title, prdRef string) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("ciam: a title is required")
	}
	var tmpl string
	switch kind {
	case ADR:
		tmpl = adrTemplate
	case PRD:
		tmpl = prdTemplate
	case Plan:
		tmpl = planTemplate
	case Research:
		tmpl = researchTemplate
	default:
		return "", fmt.Errorf("ciam: unknown document kind %q", kind)
	}
	dir := d.dir(kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	n, err := nextNumber(dir)
	if err != nil {
		return "", err
	}
	content := render(tmpl, strings.ReplaceAll(title, `"`, `'`), n, nil)
	if kind == Plan && prdRef != "" {
		content = strings.Replace(content, `prd: "PRD-000"`, fmt.Sprintf(`prd: %q`, prdRef), 1)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%03d-%s.md", kind.prefix(), n, slug(title)))
	return path, os.WriteFile(path, []byte(content), 0o644)
}

// List returns the documents of a kind ordered by number.
func (d *Docs) List(kind DocKind) ([]Doc, error) {
	dir := d.dir(kind)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Doc
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		doc := Doc{Kind: kind, Path: filepath.Join(dir, e.Name()), Title: e.Name()}
		if m := numRe.FindStringSubmatch(e.Name()); m != nil {
			doc.Number, _ = strconv.Atoi(m[1])
		}
		if fm, err := frontMatter(doc.Path); err == nil {
			if t := fm["title"]; t != "" {
				doc.Title = t
			}
			doc.Status = fm["status"]
		}
		out = append(out, doc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func frontMatter(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fm := map[string]string{}
	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan(); i++ {
		line := sc.Text()
		if i == 0 {
			if line != "---" {
				return fm, nil
			}
			continue
		}
		if line == "---" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			fm[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
		}
	}
	return fm, sc.Err()
}

func (d *Docs) find(kind DocKind, number int) (string, error) {
	ref := fmt.Sprintf("%s-%03d-", kind.prefix(), number)
	entries, err := os.ReadDir(d.dir(kind))
	if err != nil {
		return "", fmt.Errorf("ciam: cannot read %s: %w", d.dir(kind), err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ref) && strings.HasSuffix(e.Name(), ".md") {
			return filepath.Join(d.dir(kind), e.Name()), nil
		}
	}
	return "", fmt.Errorf("ciam: %s%03d not found in %s", kind.prefix()+"-", number, d.dir(kind))
}

// SetStatus rewrites the front-matter status of a document.
func (d *Docs) SetStatus(kind DocKind, number int, status string) error {
	path, err := d.find(kind, number)
	if err != nil {
		return err
	}
	return setField(path, "status", status)
}

func setField(path, field, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	in := false
	for i, l := range lines {
		if i == 0 && l == "---" {
			in = true
			continue
		}
		if in && l == "---" {
			break
		}
		if in && strings.HasPrefix(l, field+":") {
			lines[i] = field + ": " + value
			return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}
	return fmt.Errorf("ciam: %s has no %q field in its front matter", path, field)
}

// SupersedeADR marks ADR number as superseded and creates its replacement,
// which records "supersedes". Decisions are never rewritten: history is the
// point. It returns the new ADR's path.
func (d *Docs) SupersedeADR(number int, newTitle string) (string, error) {
	old, err := d.find(ADR, number)
	if err != nil {
		return "", err
	}
	if err := setField(old, "status", "superseded"); err != nil {
		return "", err
	}
	path, err := d.New(ADR, newTitle, "")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ref := fmt.Sprintf("ADR-%03d", number)
	out := strings.Replace(string(data), "\n---\n", fmt.Sprintf("\nsupersedes: %q\n---\n", ref), 1)
	return path, os.WriteFile(path, []byte(out), 0o644)
}

// PlanExcerpt returns the text of a plan (by number) for prompts, optionally
// limited to the "Fase N"/"Phase N" section.
func (d *Docs) PlanExcerpt(number int, phase string) (string, error) {
	path, err := d.find(Plan, number)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	if phase == "" {
		return text, nil
	}
	lower := strings.ToLower(text)
	start := strings.Index(lower, "### "+strings.ToLower(phase))
	if start < 0 {
		return text, nil
	}
	end := strings.Index(lower[start+4:], "\n### ")
	if end < 0 {
		return text[start:], nil
	}
	return text[start : start+4+end], nil
}

// unquote removes one pair of matching surrounding quotes.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
