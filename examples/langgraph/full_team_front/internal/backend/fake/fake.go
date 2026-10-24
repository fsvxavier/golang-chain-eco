// Package fake is the offline demo backend the PO reads: a small source tree
// (an OpenAPI document and two handlers) and an HTTP server answering the same
// API. It is test and demo data only; real runs read what the demand names.
package fake

import (
	"embed"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
)

//go:embed root
var files embed.FS

// WriteRoot copies the source tree into dir/backend and returns that path, so
// the guarded tools can read it from disk.
func WriteRoot(dir string) (string, error) {
	root := filepath.Join(dir, "backend")
	sub, err := fs.Sub(files, "root")
	if err != nil {
		return "", err
	}
	return root, os.CopyFS(root, sub)
}

// Serve starts the fake API on loopback: GET /openapi.yaml and
// GET /coupons/{code} (SAVE10 active, OLD5 expired, anything else 404).
func Serve() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		b, _ := files.ReadFile("root/openapi.yaml")
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /coupons/{code}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.PathValue("code") {
		case "SAVE10":
			_, _ = w.Write([]byte(`{"code":"SAVE10","discount_percent":10,"status":"active","expires_at":"2026-12-31T23:59:59Z"}`))
		case "OLD5":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"coupon expired or exhausted"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"coupon not found"}`))
		}
	})
	return httptest.NewServer(mux)
}
