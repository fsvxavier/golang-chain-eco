package mongo

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// E is one ordered document field; D is an ordered document. MongoDB reads
// the command name from the first field, so commands must use D.
type E struct {
	Key   string
	Value any
}
type D []E

// M is an unordered document (what decoding produces).
type M = map[string]any

func appendCString(b []byte, s string) []byte { return append(append(b, s...), 0) }

func appendDoc(b []byte, fields func(yield func(k string, v any))) ([]byte, error) {
	start := len(b)
	b = append(b, 0, 0, 0, 0)
	var err error
	fields(func(k string, v any) {
		if err == nil {
			b, err = appendElem(b, k, v)
		}
	})
	b = append(b, 0)
	binary.LittleEndian.PutUint32(b[start:], uint32(len(b)-start))
	return b, err
}

func appendElem(b []byte, k string, v any) ([]byte, error) {
	put := func(t byte) []byte { return appendCString(append(b, t), k) }
	switch x := v.(type) {
	case nil:
		return put(0x0A), nil
	case string:
		b = put(0x02)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(x)+1))
		return append(append(b, x...), 0), nil
	case bool:
		b = put(0x08)
		if x {
			return append(b, 1), nil
		}
		return append(b, 0), nil
	case int:
		return appendElem(b, k, int64(x))
	case int32:
		return binary.LittleEndian.AppendUint32(put(0x10), uint32(x)), nil
	case int64:
		return binary.LittleEndian.AppendUint64(put(0x12), uint64(x)), nil
	case float64:
		return binary.LittleEndian.AppendUint64(put(0x01), math.Float64bits(x)), nil
	case float32:
		return appendElem(b, k, float64(x))
	case time.Time:
		return binary.LittleEndian.AppendUint64(put(0x09), uint64(x.UnixMilli())), nil
	case []byte:
		b = put(0x05)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(x)))
		return append(append(b, 0), x...), nil
	case D:
		return appendDoc(put(0x03), func(y func(string, any)) {
			for _, e := range x {
				y(e.Key, e.Value)
			}
		})
	case map[string]any:
		return appendDoc(put(0x03), func(y func(string, any)) {
			for k, v := range x {
				y(k, v)
			}
		})
	case []any:
		return appendDoc(put(0x04), func(y func(string, any)) {
			for i, e := range x {
				y(fmt.Sprint(i), e)
			}
		})
	case []string:
		a := make([]any, len(x))
		for i, s := range x {
			a[i] = s
		}
		return appendElem(b, k, a)
	case []float32:
		a := make([]any, len(x))
		for i, f := range x {
			a[i] = float64(f)
		}
		return appendElem(b, k, a)
	}
	return nil, fmt.Errorf("mongo: cannot encode %T", v)
}

func marshal(d D) ([]byte, error) {
	return appendDoc(nil, func(y func(string, any)) {
		for _, e := range d {
			y(e.Key, e.Value)
		}
	})
}

func decodeDoc(b []byte) (M, int, error) {
	if len(b) < 5 {
		return nil, 0, fmt.Errorf("mongo: short bson")
	}
	n := int(binary.LittleEndian.Uint32(b))
	if n < 5 || n > len(b) {
		return nil, 0, fmt.Errorf("mongo: bad bson length %d", n)
	}
	out := M{}
	p := 4
	for p < n-1 {
		t := b[p]
		p++
		e := p
		for b[e] != 0 {
			e++
		}
		key := string(b[p:e])
		p = e + 1
		v, used, err := decodeVal(t, b[p:n])
		if err != nil {
			return nil, 0, err
		}
		out[key], p = v, p+used
	}
	return out, n, nil
}

func decodeVal(t byte, b []byte) (any, int, error) {
	short := fmt.Errorf("mongo: truncated bson value (type %#x)", t)
	need := func(n int) bool { return len(b) >= n }
	switch t {
	case 0x01:
		if !need(8) {
			return nil, 0, short
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(b)), 8, nil
	case 0x02:
		if !need(4) {
			return nil, 0, short
		}
		n := int(binary.LittleEndian.Uint32(b))
		if n < 1 || !need(4+n) {
			return nil, 0, short
		}
		return string(b[4 : 4+n-1]), 4 + n, nil
	case 0x03, 0x04:
		d, n, err := decodeDoc(b)
		if err != nil || t == 0x03 {
			return d, n, err
		}
		arr := make([]any, len(d))
		for k, v := range d {
			var i int
			fmt.Sscan(k, &i)
			if i >= 0 && i < len(arr) {
				arr[i] = v
			}
		}
		return arr, n, nil
	case 0x05:
		if !need(5) {
			return nil, 0, short
		}
		n := int(binary.LittleEndian.Uint32(b))
		if !need(5 + n) {
			return nil, 0, short
		}
		return append([]byte(nil), b[5:5+n]...), 5 + n, nil
	case 0x07:
		if !need(12) {
			return nil, 0, short
		}
		return fmt.Sprintf("%x", b[:12]), 12, nil
	case 0x08:
		if !need(1) {
			return nil, 0, short
		}
		return b[0] == 1, 1, nil
	case 0x09:
		if !need(8) {
			return nil, 0, short
		}
		return time.UnixMilli(int64(binary.LittleEndian.Uint64(b))), 8, nil
	case 0x0A:
		return nil, 0, nil
	case 0x10:
		if !need(4) {
			return nil, 0, short
		}
		return int32(binary.LittleEndian.Uint32(b)), 4, nil
	case 0x11, 0x12:
		if !need(8) {
			return nil, 0, short
		}
		return int64(binary.LittleEndian.Uint64(b)), 8, nil
	}
	return nil, 0, fmt.Errorf("mongo: unsupported bson type %#x", t)
}

// num coerces any BSON number to float64.
func num(v any) float64 {
	switch x := v.(type) {
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return 0
}
