package ciam

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectMap returns a structural overview so an assistant can orient itself
// before searching: for Django, every app with short summaries of its
// models/views/urls/serializers; otherwise, the directories that hold code
// with their file counts and main languages (and Go package names).
func ProjectMap(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	ptype := DetectType(root)
	fmt.Fprintf(&b, "# Project map: %s (%s)\n\n", filepath.Base(root), ptype)
	if ptype == "django" {
		return djangoMap(root, &b)
	}
	type dirInfo struct {
		files int
		exts  map[string]int
		pkg   string
	}
	dirs := map[string]*dirInfo{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || skipDirs[d.Name()]) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := extOf(d.Name())
		if !IndexableExtensions[ext] || ext == ".md" || ext == ".json" || ext == ".txt" {
			return nil
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		rel = filepath.ToSlash(rel)
		di := dirs[rel]
		if di == nil {
			di = &dirInfo{exts: map[string]int{}}
			dirs[rel] = di
		}
		di.files++
		di.exts[ext]++
		if ext == ".go" && di.pkg == "" && !strings.HasSuffix(d.Name(), "_test.go") {
			if data, err := os.ReadFile(path); err == nil {
				for _, l := range strings.Split(string(data), "\n") {
					if strings.HasPrefix(l, "package ") {
						di.pkg = strings.TrimSpace(strings.TrimPrefix(l, "package "))
						break
					}
				}
			}
		}
		return nil
	})
	names := make([]string, 0, len(dirs))
	for n := range dirs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		di := dirs[n]
		var exts []string
		for e, c := range di.exts {
			exts = append(exts, fmt.Sprintf("%s×%d", e, c))
		}
		sort.Strings(exts)
		label := n
		if label == "." {
			label = "(root)"
		}
		pkg := ""
		if di.pkg != "" {
			pkg = " package " + di.pkg
		}
		fmt.Fprintf(&b, "- `%s`%s — %d files (%s)\n", label, pkg, di.files, strings.Join(exts, ", "))
	}
	return b.String(), nil
}

func djangoMap(root string, b *strings.Builder) (string, error) {
	summary := func(dir, file string) string {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return ""
		}
		s := Compress(string(data), ".py")
		if len(s) > 400 {
			s = s[:400] + "…"
		}
		return s
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path != root && (strings.HasPrefix(d.Name(), ".") || skipDirs[d.Name()]) {
			return filepath.SkipDir
		}
		if _, err := os.Stat(filepath.Join(path, "apps.py")); err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		fmt.Fprintf(b, "## app `%s`\n", filepath.ToSlash(rel))
		for _, f := range []string{"models.py", "views.py", "urls.py", "serializers.py"} {
			if s := summary(path, f); s != "" {
				fmt.Fprintf(b, "### %s\n```python\n%s\n```\n", f, s)
			}
		}
		return nil
	})
	return b.String(), nil
}
