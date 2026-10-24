package skills

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"testing"
)

type lockEntry struct {
	Role, Source, Skill, Installs, Hash, License string
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
		if len(b) > 8<<10 && !e.Trimmed {
			t.Errorf("%s is %d bytes and not marked trimmed", e.file(), len(b))
		}
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
	embedded, err := fs.Glob(FS, "*/references/*/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range embedded {
		if !locked[f] {
			t.Errorf("%s is embedded but not in skills.lock", f)
		}
		delete(locked, f)
	}
	for f := range locked {
		t.Errorf("%s is in skills.lock but missing", f)
	}
}

func TestNoticesCoverLockedLicensesAndSources(t *testing.T) {
	b, err := FS.ReadFile("THIRD_PARTY_NOTICES.md")
	if err != nil {
		t.Fatal(err)
	}
	notices := string(b)
	for _, e := range readLock(t) {
		if !strings.Contains(notices, e.License) {
			t.Errorf("licence %q of %s is not in THIRD_PARTY_NOTICES.md", e.License, e.Skill)
		}
		repo, _, _ := strings.Cut(e.Source, "@")
		if !strings.Contains(notices, "github.com/"+repo) {
			t.Errorf("source repo %q of %s is not in THIRD_PARTY_NOTICES.md", repo, e.Skill)
		}
		if !strings.Contains(notices, "`"+e.Skill+"`") {
			t.Errorf("skill %q is not named in THIRD_PARTY_NOTICES.md", e.Skill)
		}
	}
}
