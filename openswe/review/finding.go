package review

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

// Severity of a finding.
type Severity string

// Severities, lowest to highest.
const (
	Low      Severity = "low"
	Medium   Severity = "medium"
	High     Severity = "high"
	Critical Severity = "critical"
)

// Rank orders severities (low=0 ... critical=3); unknown is -1.
func (s Severity) Rank() int {
	switch s {
	case Low:
		return 0
	case Medium:
		return 1
	case High:
		return 2
	case Critical:
		return 3
	}
	return -1
}

// Confidence in a finding.
type Confidence string

// Confidence levels. They are recorded for calibration; they do not gate
// publication (the defensibility bar in the prompt does).
const (
	ConfLow    Confidence = "low"
	ConfMedium Confidence = "medium"
	ConfHigh   Confidence = "high"
)

// Status of a finding.
type Status string

// Finding statuses.
const (
	Open      Status = "open"
	Resolved  Status = "resolved"
	Dismissed Status = "dismissed"
)

// Surface tracks where a finding is on GitHub. It only moves forward.
type Surface string

// Surface states.
const (
	NotSurfaced    Surface = "not_surfaced"
	Surfaced       Surface = "surfaced"
	ResolvePending Surface = "resolve_pending"
	SurfaceDone    Surface = "resolved"
)

// Interaction is a human or bot reply on a finding's thread.
type Interaction struct {
	Kind      string    `json:"kind"` // human_reply | bot_reply
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CommentID int64     `json:"comment_id"`
	At        time.Time `json:"at"`
}

// Finding is one review comment anchored to the diff.
type Finding struct {
	ID           string        `json:"id"`
	Severity     Severity      `json:"severity"`
	Confidence   Confidence    `json:"confidence"`
	Category     string        `json:"category"`
	File         string        `json:"file"`
	StartLine    int           `json:"start_line"`
	EndLine      int           `json:"end_line"`
	Side         string        `json:"side"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Suggestion   string        `json:"suggestion,omitempty"`
	DiffHunk     string        `json:"diff_hunk,omitempty"`
	HeadSHA      string        `json:"head_sha"`
	Fingerprint  string        `json:"fingerprint"`
	Status       Status        `json:"status"`
	Note         string        `json:"note,omitempty"`
	Surface      Surface       `json:"surface"`
	CommentID    int64         `json:"comment_id,omitempty"`
	ThreadID     string        `json:"thread_id,omitempty"`
	ReviewID     int64         `json:"review_id,omitempty"`
	Interactions []Interaction `json:"interactions,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
}

// Limits that keep review comments scannable.
const (
	MaxTitleLength      = 120
	MaxSuggestionLines  = 4
	DefaultFindingTitle = "Code review finding"
)

// NormalizeTitle makes a one-line headline of at most MaxTitleLength
// characters; with no usable title it falls back to the first line of the
// description, then to DefaultFindingTitle.
func NormalizeTitle(title, description string) string {
	raw := strings.TrimSpace(title)
	if raw == "" {
		raw = strings.TrimSpace(strings.SplitN(strings.TrimSpace(description), "\n", 2)[0])
	}
	compact := strings.Join(strings.Fields(raw), " ")
	if compact == "" {
		return DefaultFindingTitle
	}
	if r := []rune(compact); len(r) > MaxTitleLength {
		return strings.TrimRight(string(r[:MaxTitleLength-3]), " ") + "..."
	}
	return compact
}

// ClipSuggestion drops suggestions longer than MaxSuggestionLines: a
// one-click suggestion that rewrites the author's code reads as noise. The
// description is still posted; dropped reports that a suggestion was removed.
func ClipSuggestion(s string) (kept string, dropped bool) {
	if s == "" {
		return "", false
	}
	if strings.Count(s, "\n")+1 > MaxSuggestionLines {
		return "", true
	}
	return s, false
}

var ws = regexp.MustCompile(`\s+`)

// Fingerprint identifies "the same problem" across re-reviews, independent of
// line numbers (which move) and wording (which varies): file, category and
// normalised title.
func Fingerprint(file, category, title string) string {
	norm := strings.ToLower(ws.ReplaceAllString(strings.TrimSpace(file+"|"+category+"|"+title), " "))
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:8])
}

var markerRe = regexp.MustCompile(`<!--\s*openswe-finding:([A-Za-z0-9_-]+)\s*-->`)

// Marker is the hidden tag that links a GitHub comment to its finding.
func Marker(id string) string { return "<!-- openswe-finding:" + id + " -->" }

// ParseMarker extracts the finding id from a comment body.
func ParseMarker(body string) (string, bool) {
	m := markerRe.FindStringSubmatch(body)
	if m == nil {
		return "", false
	}
	return m[1], true
}
