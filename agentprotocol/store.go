package agentprotocol

import (
	"net/http"
	"slices"
	"strings"

	"github.com/fsvxavier/golang-chain-eco/langgraph/store"
)

func toItem(it store.Item) Item {
	return Item{Namespace: it.Namespace, Key: it.Key, Value: it.Value, CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt}
}

func (s *Server) storePut(w http.ResponseWriter, r *http.Request) {
	var req StorePutRequest
	if err := decodeBody(r, &req); err != nil || req.Key == "" || req.Value == nil {
		writeErr(w, 422, "invalid_request", "namespace, key and value are required")
		return
	}
	if err := s.opts.Store.Put(r.Context(), req.Namespace, req.Key, req.Value); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) storeDelete(w http.ResponseWriter, r *http.Request) {
	var req StoreDeleteRequest
	if err := decodeBody(r, &req); err != nil || req.Key == "" {
		writeErr(w, 422, "invalid_request", "key is required")
		return
	}
	it, err := s.opts.Store.Get(r.Context(), req.Namespace, req.Key)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	if it == nil {
		writeErr(w, 404, "not_found", "item not found")
		return
	}
	if err := s.opts.Store.Delete(r.Context(), req.Namespace, req.Key); err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) storeGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	key := q.Get("key")
	if key == "" {
		writeErr(w, 400, "invalid_request", "key is required")
		return
	}
	ns := q["namespace"] // repeated parameter, per the spec
	if len(ns) == 1 && strings.Contains(ns[0], ".") {
		ns = strings.Split(ns[0], ".") // LangGraph SDKs send one dot-separated value
	}
	it, err := s.opts.Store.Get(r.Context(), ns, key)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	if it == nil {
		writeErr(w, 404, "not_found", "item not found")
		return
	}
	writeJSON(w, 200, toItem(*it))
}

func (s *Server) storeSearch(w http.ResponseWriter, r *http.Request) {
	var req StoreSearchRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 422, "invalid_request", err.Error())
		return
	}
	items, err := s.opts.Store.Search(r.Context(), req.NamespacePrefix, req.Filter, 0)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	out := make([]Item, 0, len(items))
	for _, it := range page(items, req.Limit, req.Offset) {
		out = append(out, toItem(it))
	}
	writeJSON(w, 200, SearchItemsResponse{Items: out})
}

func (s *Server) storeNamespaces(w http.ResponseWriter, r *http.Request) {
	var req StoreListNamespacesRequest
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 422, "invalid_request", err.Error())
		return
	}
	all, err := s.opts.Store.ListNamespaces(r.Context(), req.Prefix)
	if err != nil {
		writeErr(w, 500, "internal", err.Error())
		return
	}
	out := [][]string{}
	seen := map[string]bool{}
	for _, ns := range all {
		if len(req.Suffix) > 0 && !(len(ns) >= len(req.Suffix) && slices.Equal(ns[len(ns)-len(req.Suffix):], req.Suffix)) {
			continue
		}
		if req.MaxDepth > 0 && len(ns) > req.MaxDepth {
			ns = ns[:req.MaxDepth]
		}
		if k := strings.Join(ns, "\x00"); !seen[k] {
			seen[k] = true
			out = append(out, ns)
		}
	}
	writeJSON(w, 200, page(out, req.Limit, req.Offset))
}
