// Package documents defines Document and document loaders
// (langchain_core.documents / document_loaders).
package documents

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Document is a piece of text with metadata.
type Document struct {
	ID          string         `json:"id,omitempty"`
	PageContent string         `json:"page_content"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Loader loads documents from a source (BaseLoader).
type Loader interface {
	Load(ctx context.Context) ([]Document, error)
}

// LoaderFunc adapts a function to Loader.
type LoaderFunc func(ctx context.Context) ([]Document, error)

// Load implements Loader.
func (f LoaderFunc) Load(ctx context.Context) ([]Document, error) { return f(ctx) }

// Text loads a whole file as one document (TextLoader).
func Text(path string) Loader {
	return LoaderFunc(func(context.Context) ([]Document, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []Document{{PageContent: string(b), Metadata: map[string]any{"source": path}}}, nil
	})
}

// Reader loads all of r as one document.
func Reader(r io.Reader, source string) Loader {
	return LoaderFunc(func(context.Context) ([]Document, error) {
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return []Document{{PageContent: string(b), Metadata: map[string]any{"source": source}}}, nil
	})
}

// CSV loads one document per row, rendered as "column: value" lines
// (CSVLoader). The first row is the header.
func CSV(path string) Loader {
	return LoaderFunc(func(context.Context) ([]Document, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		rows, err := csv.NewReader(f).ReadAll()
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
		var docs []Document
		for i, row := range rows[1:] {
			var lines []string
			for j, v := range row {
				if j < len(rows[0]) {
					lines = append(lines, rows[0][j]+": "+v)
				}
			}
			docs = append(docs, Document{PageContent: strings.Join(lines, "\n"), Metadata: map[string]any{"source": path, "row": i}})
		}
		return docs, nil
	})
}

// JSONLines loads one document per line of a .jsonl file, taking the text
// from contentKey and copying the remaining keys into metadata.
func JSONLines(path, contentKey string) Loader {
	return LoaderFunc(func(context.Context) ([]Document, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		var docs []Document
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for n := 0; sc.Scan(); n++ {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, n+1, err)
			}
			content := fmt.Sprint(rec[contentKey])
			delete(rec, contentKey)
			rec["source"], rec["seq_num"] = path, n
			docs = append(docs, Document{PageContent: content, Metadata: rec})
		}
		return docs, sc.Err()
	})
}

var tagRe = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<[^>]+>`)
var spaceRe = regexp.MustCompile(`\s+`)

// HTML loads a file and strips markup (a minimal BSHTMLLoader).
func HTML(path string) Loader {
	return LoaderFunc(func(context.Context) ([]Document, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(spaceRe.ReplaceAllString(tagRe.ReplaceAllString(string(b), " "), " "))
		return []Document{{PageContent: text, Metadata: map[string]any{"source": path}}}, nil
	})
}

// Directory loads every file under root matching glob (e.g. "*.md") with the
// loader returned by load (DirectoryLoader). load defaults to Text.
func Directory(root, glob string, load func(path string) Loader) Loader {
	if load == nil {
		load = Text
	}
	return LoaderFunc(func(ctx context.Context) ([]Document, error) {
		var docs []Document
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if ok, _ := filepath.Match(glob, d.Name()); glob != "" && !ok {
				return nil
			}
			ds, err := load(p).Load(ctx)
			if err != nil {
				return err
			}
			docs = append(docs, ds...)
			return nil
		})
		return docs, err
	})
}
