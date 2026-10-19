package ciam

import (
	"os"
	"path/filepath"
	"strings"
)

// Chunk is a piece of a project with its embedding and metadata.
type Chunk struct {
	Project   string    `json:"project"`
	File      string    `json:"file"` // path relative to the project root
	Index     int       `json:"index"`
	Type      string    `json:"type"` // model, view, test, adr, generic...
	Content   string    `json:"content"`
	Embedding []float32 `json:"-"`
}

// Defaults for chunking, tuned for embedding models with small contexts
// (e.g. nomic-embed-text: 2048 tokens).
const (
	// MaxChunkBytes caps what is sent to the embedder.
	MaxChunkBytes = 1800
	chunkLines    = 50
	chunkOverlap  = 5
)

// EstimateTokens is the usual ~4 characters per token heuristic.
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }

// Truncate caps s at MaxChunkBytes, cutting on a line boundary.
func Truncate(s string) string {
	if len(s) <= MaxChunkBytes {
		return s
	}
	cut := s[:MaxChunkBytes]
	if i := strings.LastIndex(cut, "\n"); i > MaxChunkBytes/2 {
		cut = cut[:i]
	}
	return cut + "\n// [truncated: exceeds the embedding model context]"
}

// SplitLines cuts content into windows of maxLines lines overlapping by
// overlap lines, dropping blank windows.
func SplitLines(content string, maxLines, overlap int) []string {
	if maxLines <= overlap {
		overlap = 0
	}
	lines := strings.Split(content, "\n")
	var out []string
	for start := 0; start < len(lines); start += maxLines - overlap {
		end := min(start+maxLines, len(lines))
		if c := strings.Join(lines[start:end], "\n"); strings.TrimSpace(c) != "" {
			out = append(out, c)
		}
		if end == len(lines) {
			break
		}
	}
	return out
}

// Classifier labels a file with its role in the project ("model", "view",
// "test"...). Return "" to fall through to the next classifier.
type Classifier func(relPath, content string) string

// Indexable extensions and directories never worth indexing.
var (
	IndexableExtensions = map[string]bool{
		".py": true, ".go": true, ".md": true, ".txt": true, ".toml": true, ".yaml": true, ".yml": true,
		".json": true, ".html": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true, ".sh": true,
		".sql": true, ".rs": true, ".java": true, ".kt": true, ".rb": true, ".php": true, ".proto": true,
	}
	skipDirs = map[string]bool{
		".git": true, "__pycache__": true, ".venv": true, "venv": true, "node_modules": true,
		".mypy_cache": true, ".pytest_cache": true, "dist": true, "build": true, ".tox": true,
		"htmlcov": true, "vendor": true, "staticfiles": true, "media": true, "target": true,
	}
)

// Django file conventions (models.py -> model, ...).
var djangoFiles = map[string]string{
	"models.py": "model", "views.py": "view", "viewsets.py": "view", "urls.py": "url",
	"serializers.py": "serializer", "forms.py": "form", "admin.py": "admin", "signals.py": "signal",
	"managers.py": "manager", "tasks.py": "task", "apps.py": "app_config", "settings.py": "settings",
	"conftest.py": "test",
}

// Content hints used when the file name says nothing (Django).
var djangoHints = []struct{ keyword, kind string }{
	{"from django.db import models", "model"}, {"ModelSerializer", "serializer"}, {"APIView", "view"},
	{"ViewSet", "view"}, {"urlpatterns", "url"}, {"@shared_task", "task"}, {"@receiver", "signal"},
}

// DjangoClassifier knows Django project conventions.
func DjangoClassifier(rel, content string) string {
	base := strings.ToLower(filepath.Base(rel))
	if k, ok := djangoFiles[base]; ok {
		return k
	}
	if filepath.Ext(base) != ".py" {
		return ""
	}
	if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
		return "test"
	}
	for _, h := range djangoHints {
		if strings.Contains(content, h.keyword) {
			return h.kind
		}
	}
	return ""
}

// GenericClassifier labels tests, docs, config and a few Go/JS/Python roles.
func GenericClassifier(rel, content string) string {
	base := strings.ToLower(filepath.Base(rel))
	ext := filepath.Ext(base)
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasPrefix(base, "test_"), strings.Contains(base, ".test."), strings.Contains(base, ".spec."):
		return "test"
	case ext == ".md":
		return "doc"
	case ext == ".yaml" || ext == ".yml" || ext == ".toml" || ext == ".json":
		return "config"
	case ext == ".sql":
		return "schema"
	case ext == ".proto":
		return "schema"
	case ext == ".go" && strings.Contains(content, "http.Handler"):
		return "handler"
	case ext == ".go" && strings.Contains(content, "func main()"):
		return "main"
	}
	return ""
}

// DetectType guesses the project type from marker files: "django", "go",
// "node", "python" or "generic".
func DetectType(root string) string {
	has := func(name string) bool { _, err := os.Stat(filepath.Join(root, name)); return err == nil }
	switch {
	case has("manage.py"):
		return "django"
	case has("go.mod"):
		return "go"
	case has("package.json"):
		return "node"
	case has("pyproject.toml"), has("requirements.txt"), has("setup.py"):
		return "python"
	}
	return "generic"
}

// ClassifiersFor returns the classifier chain for a project type.
func ClassifiersFor(projectType string) []Classifier {
	if projectType == "django" {
		return []Classifier{DjangoClassifier, GenericClassifier}
	}
	return []Classifier{GenericClassifier}
}
