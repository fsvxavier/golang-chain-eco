// Package review is the pull-request review system: diff-anchored findings, a
// findings store that survives re-reviews, the tools a reviewer agent calls to
// record and publish them, reconciliation with GitHub review threads, and
// per-repository review styles.
package review

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Hunk is one hunk of a file diff. NewStart..NewEnd are inclusive line numbers
// of the post-change (RIGHT) side; Body is the raw hunk text including "@@".
type Hunk struct {
	OldStart, OldEnd int
	NewStart, NewEnd int
	Body             string
}

// FileDiff holds the commentable lines of one file.
type FileDiff struct {
	Path string
	// Right are lines (new-file numbering) that are added or context; Left are
	// removed or context lines (old-file numbering).
	Right, Left map[int]bool
	Hunks       []Hunk
	Deleted     bool
}

// Diff is a parsed unified diff.
type Diff struct {
	Files map[string]*FileDiff
	Order []string
}

var (
	fileHeader = regexp.MustCompile(`^diff --git a/(.+?) b/(.+)$`)
	hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
)

// ParseDiff parses a unified diff (as produced by git or GitHub).
func ParseDiff(text string) *Diff {
	d := &Diff{Files: map[string]*FileDiff{}}
	var cur *FileDiff
	var hunk *Hunk
	var hunkLines []string
	flush := func() {
		if cur != nil && hunk != nil {
			hunk.Body = strings.Join(hunkLines, "\n")
			cur.Hunks = append(cur.Hunks, *hunk)
		}
		hunk, hunkLines = nil, nil
	}
	var oldLn, newLn int
	for _, line := range strings.Split(text, "\n") {
		if m := fileHeader.FindStringSubmatch(line); m != nil {
			flush()
			cur = &FileDiff{Path: m[2], Right: map[int]bool{}, Left: map[int]bool{}}
			if _, dup := d.Files[cur.Path]; !dup {
				d.Order = append(d.Order, cur.Path)
			}
			d.Files[cur.Path] = cur
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, "+++ /dev/null") {
			cur.Deleted = true
			continue
		}
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			flush()
			os, _ := strconv.Atoi(m[1])
			oc := atoiDefault(m[2], 1)
			ns, _ := strconv.Atoi(m[3])
			nc := atoiDefault(m[4], 1)
			hunk = &Hunk{OldStart: os, OldEnd: os + max(oc, 1) - 1, NewStart: ns, NewEnd: ns + max(nc, 1) - 1}
			hunkLines = []string{line}
			oldLn, newLn = os, ns
			continue
		}
		if hunk == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			cur.Right[newLn] = true
			newLn++
		case strings.HasPrefix(line, "-"):
			cur.Left[oldLn] = true
			oldLn++
		case strings.HasPrefix(line, `\`): // "\ No newline at end of file"
		default: // context
			cur.Right[newLn] = true
			cur.Left[oldLn] = true
			newLn++
			oldLn++
		}
		hunkLines = append(hunkLines, line)
	}
	flush()
	return d
}

func atoiDefault(s string, d int) int {
	if s == "" {
		return d
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return d
	}
	return n
}

// Contains reports whether every line of [start,end] on side ("RIGHT" or
// "LEFT") belongs to the diff of file, which is what GitHub requires of an
// inline comment.
func (d *Diff) Contains(file string, start, end int, side string) bool {
	f := d.Files[file]
	if f == nil || start <= 0 || end < start {
		return false
	}
	set := f.Right
	if side == "LEFT" {
		set = f.Left
	}
	for ln := start; ln <= end; ln++ {
		if !set[ln] {
			return false
		}
	}
	return true
}

// HunkFor returns the hunk text covering the first line of a range.
func (d *Diff) HunkFor(file string, start int) string {
	f := d.Files[file]
	if f == nil {
		return ""
	}
	for _, h := range f.Hunks {
		if start >= h.NewStart && start <= h.NewEnd {
			return h.Body
		}
	}
	return ""
}

// Paths lists changed files in diff order.
func (d *Diff) Paths() []string { return slices.Clone(d.Order) }

// Stats summarises the diff for prompts.
func (d *Diff) Stats() (files, addedLines int) {
	files = len(d.Files)
	for _, f := range d.Files {
		for _, h := range f.Hunks {
			for _, l := range strings.Split(h.Body, "\n")[1:] {
				if strings.HasPrefix(l, "+") {
					addedLines++
				}
			}
		}
	}
	return
}

// SortedPaths returns the paths alphabetically.
func (d *Diff) SortedPaths() []string {
	p := d.Paths()
	sort.Strings(p)
	return p
}
