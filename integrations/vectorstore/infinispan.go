package vectorstore

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// Infinispan stores vectors in an indexed cache (Infinispan 15+, Protobuf
// schema with @Vector) through the REST API, searched with the Ickle KNN
// operator (<->); hits carry no embedding (multi-valued fields cannot be
// projected). The server's REST endpoint uses Digest authentication,
// which this client negotiates.
type Infinispan struct {
	URL      string // http://localhost:11222
	Username string
	Password string
	Cache    string // default "lcg_vectors"
	HTTP     *http.Client

	mu        sync.Mutex
	challenge map[string]string
	nc        int
	ensured   bool
}

var digestParam = regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([^,\s]+))`)

func (s *Infinispan) cache() string {
	if s.Cache == "" {
		return "lcg_vectors"
	}
	return s.Cache
}

func (s *Infinispan) newHash() (hash.Hash, string) {
	if strings.EqualFold(s.challenge["algorithm"], "SHA-256") {
		return sha256.New(), "SHA-256"
	}
	return md5.New(), "MD5"
}

func (s *Infinispan) digestHeader(method, uri string) string {
	h := func(x string) string {
		hh, _ := s.newHash()
		io.WriteString(hh, x)
		return hex.EncodeToString(hh.Sum(nil))
	}
	_, alg := s.newHash()
	s.nc++
	nc := fmt.Sprintf("%08x", s.nc)
	cb := make([]byte, 8)
	rand.Read(cb)
	cnonce := hex.EncodeToString(cb)
	ha1 := h(s.Username + ":" + s.challenge["realm"] + ":" + s.Password)
	ha2 := h(method + ":" + uri)
	resp := h(strings.Join([]string{ha1, s.challenge["nonce"], nc, cnonce, "auth", ha2}, ":"))
	out := fmt.Sprintf(`Digest username=%q, realm=%q, nonce=%q, uri=%q, algorithm=%s, response=%q, qop=auth, nc=%s, cnonce=%q`,
		s.Username, s.challenge["realm"], s.challenge["nonce"], uri, alg, resp, nc, cnonce)
	if op := s.challenge["opaque"]; op != "" {
		out += fmt.Sprintf(`, opaque=%q`, op)
	}
	return out
}

func (s *Infinispan) do(ctx context.Context, method, path, ctype string, body []byte) (int, []byte, error) {
	hc := s.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	full := strings.TrimRight(s.URL, "/") + path
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, full, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		s.mu.Lock()
		if s.Username != "" && s.challenge != nil {
			req.Header.Set("Authorization", s.digestHeader(method, req.URL.RequestURI()))
		}
		s.mu.Unlock()
		resp, err := hc.Do(req)
		if err != nil {
			return 0, nil, err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 && s.Username != "" {
			if wa := resp.Header.Get("Www-Authenticate"); strings.HasPrefix(wa, "Digest") {
				ch := map[string]string{}
				for _, m := range digestParam.FindAllStringSubmatch(wa, -1) {
					ch[strings.ToLower(m[1])] = m[2] + m[3]
				}
				s.mu.Lock()
				s.challenge = ch
				s.mu.Unlock()
				continue
			}
		}
		return resp.StatusCode, raw, nil
	}
	return 0, nil, fmt.Errorf("infinispan: authentication failed")
}

func (s *Infinispan) must(ctx context.Context, method, path, ctype string, body []byte, okCodes ...int) ([]byte, error) {
	st, raw, err := s.do(ctx, method, path, ctype, body)
	if err != nil {
		return nil, err
	}
	for _, c := range okCodes {
		if st == c {
			return raw, nil
		}
	}
	if st/100 == 2 {
		return raw, nil
	}
	return raw, fmt.Errorf("infinispan: %s %s: http %d: %s", method, path, st, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
}

func (s *Infinispan) ensure(ctx context.Context, dim int) error {
	if s.ensured {
		return nil
	}
	schema := fmt.Sprintf(`package lcg;

/** @Indexed */
message %[1]s {
  /** @Keyword */
  optional string id = 1;
  /** @Text */
  optional string text = 2;
  optional string metadata = 3;
  /** @Vector(dimension=%[2]d, similarity=COSINE) */
  repeated float embedding = 4;
}
`, s.entity(dim), dim)
	if _, err := s.must(ctx, http.MethodPost, fmt.Sprintf("/rest/v2/schemas/lcg%d.proto", dim), "text/plain", []byte(schema), 409); err != nil {
		return err
	}
	cfg := `{"distributed-cache":{"mode":"SYNC","encoding":{"media-type":"application/x-protostream"},"indexing":{"enabled":true,"storage":"local-heap","indexed-entities":["lcg.%s"]}}}`
	if _, err := s.must(ctx, http.MethodPost, "/rest/v2/caches/"+url.PathEscape(s.cache()), "application/json", []byte(fmt.Sprintf(cfg, s.entity(dim))), 400); err != nil {
		return err
	}
	s.ensured = true
	return nil
}

// entity is the Protobuf message name; the vector dimension is part of the
// schema, so each dimension gets its own type.
func (s *Infinispan) entity(dim int) string { return fmt.Sprintf("Chunk%d", dim) }

func (s *Infinispan) key(id string) string {
	return "/rest/v2/caches/" + url.PathEscape(s.cache()) + "/" + url.PathEscape(id)
}

func (s *Infinispan) Upsert(ctx context.Context, recs []Record) error {
	if len(recs) == 0 {
		return nil
	}
	if err := s.ensure(ctx, len(recs[0].Vector)); err != nil {
		return err
	}
	for _, r := range recs {
		md, _ := json.Marshal(r.Metadata)
		b, _ := json.Marshal(map[string]any{"_type": "lcg." + s.entity(len(r.Vector)), "id": r.ID, "text": r.Text, "metadata": string(md), "embedding": r.Vector})
		if _, err := s.must(ctx, http.MethodPut, s.key(r.ID), "application/json", b); err != nil {
			return err
		}
	}
	return nil
}

func (s *Infinispan) Query(ctx context.Context, vec []float32, k int) ([]Hit, error) {
	vb, _ := json.Marshal(vec)
	q := fmt.Sprintf("select c.id, c.text, c.metadata, score(c) from lcg.%s c where c.embedding <-> %s~%d", s.entity(len(vec)), vb, k)
	st, raw, err := s.do(ctx, http.MethodGet, "/rest/v2/caches/"+url.PathEscape(s.cache())+"?action=search&query="+url.QueryEscape(q), "", nil)
	if err != nil {
		return nil, err
	}
	if st == http.StatusNotFound || (st == http.StatusBadRequest && strings.Contains(string(raw), "Unknown entity")) {
		return nil, nil
	}
	if st/100 != 2 {
		return nil, fmt.Errorf("infinispan: query: http %d: %s", st, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	var out struct {
		Hits []struct {
			Hit struct {
				ID        string    `json:"id"`
				Text      string    `json:"text"`
				Metadata  string    `json:"metadata"`
				Embedding []float32 `json:"embedding"`
				Score     float64   `json:"score()"`
			} `json:"hit"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	var hits []Hit
	for _, h := range out.Hits {
		x := Hit{Record: Record{ID: h.Hit.ID, Text: h.Hit.Text, Vector: h.Hit.Embedding}, Score: h.Hit.Score}
		_ = json.Unmarshal([]byte(h.Hit.Metadata), &x.Metadata)
		hits = append(hits, x)
	}
	return hits, nil
}

func (s *Infinispan) Delete(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if _, err := s.must(ctx, http.MethodDelete, s.key(id), "", nil, 404); err != nil {
			return err
		}
	}
	return nil
}
