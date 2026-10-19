package ciam

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"

	"github.com/fsvxavier/golang-chain-eco/core/tools"
)

// Handler serves the toolkit over plain HTTP JSON, for clients that are not
// MCP hosts:
//
//	POST /tools/{name}   body = tool arguments, response = tool result
//	GET  /tools          names, descriptions and parameter schemas
//	GET  /status         index and usage counters (Manager.Status)
//	GET  /healthz        liveness, never authenticated
//
// When apiKey is set every other route requires it in the X-Api-Key header.
func (k Toolkit) Handler(apiKey string) http.Handler {
	ts := k.All()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /tools/{name}", func(w http.ResponseWriter, r *http.Request) {
		t, ok := tools.Find(ts, r.PathValue("name"))
		if !ok {
			writeAPIErr(w, http.StatusNotFound, "unknown tool "+r.PathValue("name"))
			return
		}
		args := map[string]any{}
		if raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)); err != nil {
			writeAPIErr(w, http.StatusBadRequest, err.Error())
			return
		} else if len(raw) > 0 {
			if err := json.Unmarshal(raw, &args); err != nil {
				writeAPIErr(w, http.StatusBadRequest, "body must be a JSON object: "+err.Error())
				return
			}
		}
		out, err := t.Call(r.Context(), args)
		if err != nil {
			code := http.StatusInternalServerError
			if tools.IsToolError(err) {
				code = http.StatusBadRequest
			}
			writeAPIErr(w, code, err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /tools", func(w http.ResponseWriter, _ *http.Request) {
		writeAPIJSON(w, http.StatusOK, tools.DefineAll(ts))
	})

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		st, err := k.M.Status(r.Context())
		if err != nil {
			writeAPIErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeAPIJSON(w, http.StatusOK, st)
	})

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeAPIJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	if apiKey == "" {
		return mux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Api-Key")), []byte(apiKey)) != 1 {
			writeAPIErr(w, http.StatusUnauthorized, "missing or invalid X-Api-Key")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func writeAPIJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIErr(w http.ResponseWriter, code int, msg string) {
	writeAPIJSON(w, code, map[string]string{"error": msg})
}
