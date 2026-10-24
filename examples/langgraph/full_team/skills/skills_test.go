package skills

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

type lockEntry struct {
	Role, Source, Skill, Installs, Hash, License string
	PinnedFrom                                   string `json:"pinned_from"`
	Trimmed                                      bool
}

func readLock(t *testing.T) []lockEntry {
	t.Helper()
	f, err := FS.Open("skills.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []lockEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if sc.Text() == "" {
			continue
		}
		var e lockEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("bad lock line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		t.Fatal("skills.lock is empty")
	}
	return out
}

func (e lockEntry) file() string { return path.Join(e.Role, "references", e.Skill, "SKILL.md") }

func TestEveryLockEntryIsEmbedded(t *testing.T) {
	for _, e := range readLock(t) {
		b, err := FS.ReadFile(e.file())
		if err != nil {
			t.Errorf("%s: %v", e.file(), err)
			continue
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != e.Hash {
			t.Errorf("%s: hash %s, lock says %s", e.file(), got, e.Hash)
		}
	}
}

func TestVendoredSkillWithinBudget(t *testing.T) {
	perRole := map[string]int{}
	for _, e := range readLock(t) {
		b, err := FS.ReadFile(e.file())
		if err != nil {
			t.Fatal(err)
		}
		perRole[e.Role] += len(b)
		if len(b) > 8<<10 {
			t.Errorf("%s is %d bytes, over the 8 KB budget", e.file(), len(b))
		}
	}
	for role, n := range perRole {
		if n > 24<<10 {
			t.Errorf("%s references total %d bytes, over the 24 KB budget", role, n)
		}
	}
}

func TestEveryLockEntryHasLicense(t *testing.T) {
	for _, e := range readLock(t) {
		if e.License == "" || e.Source == "" {
			t.Errorf("%s: license %q source %q", e.file(), e.License, e.Source)
		}
	}
}

// Every embedded reference must be pinned in the lock, and the other way round.
func TestEveryRoleDirHasOwnCopies(t *testing.T) {
	locked := map[string]bool{}
	for _, e := range readLock(t) {
		locked[e.file()] = true
	}
	err := fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || !strings.Contains(p, "/references/") && !strings.HasSuffix(p, "/references") {
			return err
		}
		if d.IsDir() {
			if p == path.Join(strings.Split(p, "/")[0], "references") {
				return nil
			}
			if ents, _ := fs.ReadDir(FS, p); len(ents) != 1 || ents[0].Name() != "SKILL.md" {
				t.Errorf("%s must hold exactly SKILL.md", p)
			}
			return nil
		}
		if !locked[p] {
			t.Errorf("%s is embedded but not in skills.lock", p)
		}
		delete(locked, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for f := range locked {
		t.Errorf("%s is in skills.lock but missing", f)
	}
}

// Distinct phrases of the standard texts; each licence text must appear once.
var licenceText = map[string]string{
	"MIT": "Permission is hereby granted, free of charge",
}

func TestNoticesCoverLockedLicensesAndSources(t *testing.T) {
	b, err := FS.ReadFile("THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	notices := string(b)
	rows := map[string]string{} // repo -> its table row
	for _, l := range strings.Split(notices, "\n") {
		if strings.HasPrefix(l, "| https://github.com/") {
			repo := strings.TrimPrefix(strings.SplitN(l, " |", 2)[0], "| https://github.com/")
			rows[repo] = l
		}
	}
	seen := map[string]bool{}
	for _, e := range readLock(t) {
		repo, _, _ := strings.Cut(e.Source, "@")
		row, ok := rows[repo]
		switch {
		case !ok:
			t.Errorf("no source row for %s (%s)", repo, e.Skill)
		case !strings.Contains(row, "`"+e.Skill+"`"):
			t.Errorf("row of %s does not name skill %q", repo, e.Skill)
		case !strings.Contains(row, "| "+e.License+" |"):
			t.Errorf("row of %s does not carry licence %q", repo, e.License)
		}
		if seen[e.License] {
			continue
		}
		seen[e.License] = true
		if !strings.Contains(notices, "### "+e.License+"\n") {
			t.Errorf("no heading for licence %q", e.License)
		}
		phrase, known := licenceText[e.License]
		if !known {
			t.Errorf("test has no text marker for licence %q", e.License)
		} else if n := strings.Count(notices, phrase); n != 1 {
			t.Errorf("licence %q text appears %d times, want 1", e.License, n)
		}
	}
}

func TestPinnedFromIsPlausibleCommit(t *testing.T) {
	commit := regexp.MustCompile(`/(?:blob|tree)/[0-9a-f]{7,40}(?:/|$)`)
	must := map[string]bool{"user-stories": true}
	for _, e := range readLock(t) {
		if must[e.Skill] {
			if !commit.MatchString(e.PinnedFrom) {
				t.Errorf("%s: pinned_from %q is not a commit-pinned URL", e.Skill, e.PinnedFrom)
			}
			delete(must, e.Skill)
		}
	}
	for s := range must {
		t.Errorf("%s is not in the lock", s)
	}
}
