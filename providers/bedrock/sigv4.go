package bedrock

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Credentials are AWS access keys (SessionToken optional).
type Credentials struct {
	AccessKeyID, SecretAccessKey, SessionToken string
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func hexSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Sign adds AWS Signature Version 4 headers to req (Host, X-Amz-Date,
// optional X-Amz-Security-Token and Authorization). body must be the exact
// request payload.
func Sign(req *http.Request, body []byte, c Credentials, region, service string, t time.Time) {
	t = t.UTC()
	amzDate, day := t.Format("20060102T150405Z"), t.Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	if c.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.SessionToken)
	}
	host := req.URL.Host
	req.Host = host

	// canonical headers: everything currently set, plus host
	hdr := map[string]string{"host": host}
	for k, v := range req.Header {
		if strings.EqualFold(k, "Authorization") {
			continue
		}
		hdr[strings.ToLower(k)] = strings.Join(strings.Fields(strings.Join(v, ",")), " ")
	}
	names := make([]string, 0, len(hdr))
	for k := range hdr {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + hdr[k] + "\n")
	}
	signed := strings.Join(names, ";")

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	// AWS (except S3) double-encodes path segments
	segs := strings.Split(path, "/")
	for i, s := range segs {
		un, err := url.PathUnescape(s)
		if err != nil {
			un = s
		}
		segs[i] = strings.ReplaceAll(url.QueryEscape(un), "+", "%20")
	}
	canonPath := strings.Join(segs, "/")

	q := req.URL.Query()
	qk := make([]string, 0, len(q))
	for k := range q {
		qk = append(qk, k)
	}
	sort.Strings(qk)
	var qs []string
	for _, k := range qk {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			qs = append(qs, strings.ReplaceAll(url.QueryEscape(k), "+", "%20")+"="+strings.ReplaceAll(url.QueryEscape(v), "+", "%20"))
		}
	}

	canonical := strings.Join([]string{req.Method, canonPath, strings.Join(qs, "&"), canonHeaders.String(), signed, hexSHA256(body)}, "\n")
	scope := day + "/" + region + "/" + service + "/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256([]byte(canonical))

	k := hmacSHA256([]byte("AWS4"+c.SecretAccessKey), day)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.AccessKeyID+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
}
