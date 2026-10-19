package vectorstore

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
)

// FAISSFlat reads a FAISS IndexFlatL2 ("IxF2"), IndexFlatIP ("IxFI") or
// IndexIDMap ("IxMp") wrapping one of them, as written by faiss.write_index.
//
// Layout (faiss/impl/index_read.cpp and index_write.cpp, little-endian):
//
//	header: fourcc[4] | d int32 | ntotal int64 | dummy int64 | dummy int64 |
//	        is_trained uint8 | metric_type int32 | metric_arg float32 (only if metric_type > 1)
//	flat:   header | codes (uint64 float count + float32 vectors; WRITEXBVECTOR)
//	idmap:  header | nested index | ids (uint64 count + int64 each)
//
// ponytail: only the flat and idmap-over-flat kinds. IVF, PQ, HNSW and the
// other FAISS formats are rejected with an error, not parsed.
//
// Checked against files written by faiss-cpu 1.15.1 (testdata/real_*.faiss).
type FAISSFlat struct {
	D      int
	Metric string           // "L2" (squared distance) or "IP" (inner product)
	DocIDs map[int64]string // optional label -> document id; nil means the decimal label
	ids    []int64          // labels per vector; nil means the position
	vecs   [][]float32
}

var _ Backend = (*FAISSFlat)(nil)

// LoadFAISSFlat reads a FAISS flat index file.
func LoadFAISSFlat(path string) (*FAISSFlat, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseFAISSFlat(b)
}

type faissHeader struct {
	code   string
	d      int
	ntotal int64
	metric int32
}

type faissReader struct {
	b   []byte
	off int
}

func (r *faissReader) take(n uint64) ([]byte, error) {
	if n > uint64(len(r.b)-r.off) {
		return nil, fmt.Errorf("faiss: truncated file at byte %d (need %d)", r.off, n)
	}
	s := r.b[r.off : r.off+int(n)]
	r.off += int(n)
	return s, nil
}

func (r *faissReader) u64() (uint64, error) {
	s, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(s), nil
}

func (r *faissReader) header() (faissHeader, error) {
	var h faissHeader
	s, err := r.take(4)
	if err != nil {
		return h, err
	}
	h.code = string(s)
	// d int32, ntotal int64, two dummy int64, is_trained uint8, metric int32
	s, err = r.take(4 + 8 + 16 + 1 + 4)
	if err != nil {
		return h, err
	}
	h.d = int(int32(binary.LittleEndian.Uint32(s)))
	h.ntotal = int64(binary.LittleEndian.Uint64(s[4:]))
	h.metric = int32(binary.LittleEndian.Uint32(s[4+8+16+1:]))
	if h.metric > 1 {
		if _, err := r.take(4); err != nil { // metric_arg, unused for L2/IP
			return h, err
		}
	}
	if h.d <= 0 || h.ntotal < 0 {
		return h, fmt.Errorf("faiss: bad header d=%d ntotal=%d", h.d, h.ntotal)
	}
	return h, nil
}

// flatCodes reads the codes of a flat index and returns its vectors.
func (r *faissReader) flatCodes(h faissHeader) ([][]float32, error) {
	n, err := r.u64()
	if err != nil {
		return nil, err
	}
	if want := uint64(h.ntotal) * uint64(h.d); n != want {
		return nil, fmt.Errorf("faiss: codes hold %d floats, want %d (ntotal %d, d %d)", n, want, h.ntotal, h.d)
	}
	raw, err := r.take(n * 4)
	if err != nil {
		return nil, err
	}
	vecs := make([][]float32, h.ntotal)
	for i := range vecs {
		v := make([]float32, h.d)
		for j := range v {
			v[j] = math.Float32frombits(binary.LittleEndian.Uint32(raw[(i*h.d+j)*4:]))
		}
		vecs[i] = v
	}
	return vecs, nil
}

func parseFAISSFlat(b []byte) (*FAISSFlat, error) {
	r := &faissReader{b: b}
	h, err := r.header()
	if err != nil {
		return nil, err
	}
	f := &FAISSFlat{D: h.d}
	var flat faissHeader
	switch h.code {
	case "IxF2", "IxFI":
		flat = h
	case "IxMp":
		if flat, err = r.header(); err != nil {
			return nil, err
		}
		if flat.code != "IxF2" && flat.code != "IxFI" {
			return nil, fmt.Errorf("faiss: IndexIDMap wraps %q; only flat indexes are supported", flat.code)
		}
	default:
		return nil, fmt.Errorf("faiss: unsupported index %q (supported: IxF2, IxFI, IxMp over a flat index)", h.code)
	}
	switch {
	case flat.code == "IxF2" && flat.metric != 1, flat.code == "IxFI" && flat.metric != 0:
		return nil, fmt.Errorf("faiss: %s has metric_type %d", flat.code, flat.metric)
	}
	if flat.code == "IxF2" {
		f.Metric = "L2"
	} else {
		f.Metric = "IP"
	}
	if f.vecs, err = r.flatCodes(flat); err != nil {
		return nil, err
	}
	if h.code == "IxMp" {
		n, err := r.u64()
		if err != nil {
			return nil, err
		}
		if int64(n) != h.ntotal {
			return nil, fmt.Errorf("faiss: id map has %d entries, index has %d", n, h.ntotal)
		}
		raw, err := r.take(n * 8)
		if err != nil {
			return nil, err
		}
		f.ids = make([]int64, n)
		for i := range f.ids {
			f.ids[i] = int64(binary.LittleEndian.Uint64(raw[i*8:]))
		}
	}
	if r.off != len(b) {
		return nil, fmt.Errorf("faiss: %d trailing bytes after the index", len(b)-r.off)
	}
	return f, nil
}

func (f *FAISSFlat) label(i int) int64 {
	if f.ids != nil {
		return f.ids[i]
	}
	return int64(i)
}

func (f *FAISSFlat) docID(label int64) string {
	if s, ok := f.DocIDs[label]; ok {
		return s
	}
	return strconv.FormatInt(label, 10)
}

// Len is the number of vectors in the index.
func (f *FAISSFlat) Len() int { return len(f.vecs) }

// Query returns the k best vectors. Score is the inner product for "IP" and
// the negated squared distance for "L2" (higher is better, as FAISS ranks).
func (f *FAISSFlat) Query(_ context.Context, q []float32, k int) ([]Hit, error) {
	hits := make([]Hit, len(f.vecs))
	for i, v := range f.vecs {
		var s float64
		if f.Metric == "IP" {
			s = float64(dot(q, v))
		} else {
			d := dist(q, v)
			s = -d * d
		}
		hits[i] = Hit{Record{ID: f.docID(f.label(i)), Vector: v}, s}
	}
	return rankHits(hits, k), nil
}

func (f *FAISSFlat) Upsert(context.Context, []Record) error { return ErrReadOnlyIndex }
func (f *FAISSFlat) Delete(context.Context, []string) error { return ErrReadOnlyIndex }
