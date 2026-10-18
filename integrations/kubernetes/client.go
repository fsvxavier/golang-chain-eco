// Package kubernetes is a small Kubernetes API client (bearer token, no
// client-go): list pods, read logs, create/delete pods and run commands in a
// container over the exec WebSocket. On top of that, Sandbox turns a
// throwaway pod into a deepagents backend (files + shell execution).
//
//	c := &kubernetes.Client{Server: "https://10.0.0.1:6443", Token: tok, CA: caPEM}
//	sb, _ := kubernetes.NewSandbox(ctx, c, "default", "ubuntu:24.04")
//	defer sb.Close(ctx)
//	agent := deepagents.Create(model, deepagents.WithBackend(sb.Backend()))
package kubernetes

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Client talks to a Kubernetes API server.
type Client struct {
	Server   string // https://host:6443
	Token    string // service-account bearer token
	CA       []byte // PEM bundle verifying the server (empty = system roots)
	Insecure bool   // skip TLS verification (tests only)

	once sync.Once
	hc   *http.Client
	tlsc *tls.Config
}

func (c *Client) init() {
	c.once.Do(func() {
		c.tlsc = &tls.Config{InsecureSkipVerify: c.Insecure, MinVersion: tls.VersionTLS12}
		if len(c.CA) > 0 {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(c.CA)
			c.tlsc.RootCAs = pool
		}
		c.hc = &http.Client{Transport: &http.Transport{TLSClientConfig: c.tlsc}}
	})
}

// StatusError is a non-2xx API response.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("kubernetes: http %d: %s", e.Code, e.Message)
}

// IsNotFound reports a 404 from the API.
func IsNotFound(err error) bool {
	e, ok := err.(*StatusError)
	return ok && e.Code == http.StatusNotFound
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	c.init()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Server, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		var st struct{ Message string }
		_ = json.Unmarshal(raw, &st)
		if st.Message == "" {
			st.Message = strings.TrimSpace(string(raw[:min(len(raw), 300)]))
		}
		return &StatusError{resp.StatusCode, st.Message}
	}
	if s, ok := out.(*string); ok {
		*s = string(raw)
		return nil
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Pod is the slice of a pod's state agents care about.
type Pod struct {
	Name      string
	Namespace string
	Phase     string
	IP        string
	Ready     bool
}

func podFrom(raw struct {
	Metadata struct{ Name, Namespace string } `json:"metadata"`
	Status   struct {
		Phase      string `json:"phase"`
		PodIP      string `json:"podIP"`
		Conditions []struct{ Type, Status string }
	} `json:"status"`
}) Pod {
	p := Pod{Name: raw.Metadata.Name, Namespace: raw.Metadata.Namespace, Phase: raw.Status.Phase, IP: raw.Status.PodIP}
	for _, c := range raw.Status.Conditions {
		if c.Type == "Ready" && c.Status == "True" {
			p.Ready = true
		}
	}
	return p
}

// ListPods lists pods in namespace (empty = all), optionally by label selector.
func (c *Client) ListPods(ctx context.Context, namespace, labelSelector string) ([]Pod, error) {
	path := "/api/v1/pods"
	if namespace != "" {
		path = "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods"
	}
	if labelSelector != "" {
		path += "?labelSelector=" + url.QueryEscape(labelSelector)
	}
	var out struct {
		Items []struct {
			Metadata struct{ Name, Namespace string } `json:"metadata"`
			Status   struct {
				Phase      string `json:"phase"`
				PodIP      string `json:"podIP"`
				Conditions []struct{ Type, Status string }
			} `json:"status"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	pods := make([]Pod, len(out.Items))
	for i, it := range out.Items {
		pods[i] = podFrom(it)
	}
	return pods, nil
}

// GetPod returns one pod.
func (c *Client) GetPod(ctx context.Context, namespace, name string) (Pod, error) {
	var raw struct {
		Metadata struct{ Name, Namespace string } `json:"metadata"`
		Status   struct {
			Phase      string `json:"phase"`
			PodIP      string `json:"podIP"`
			Conditions []struct{ Type, Status string }
		} `json:"status"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(name), nil, &raw)
	return podFrom(raw), err
}

// Logs returns the last tail lines (0 = all) of a pod's log.
func (c *Client) Logs(ctx context.Context, namespace, pod, container string, tail int) (string, error) {
	q := url.Values{}
	if tail > 0 {
		q.Set("tailLines", fmt.Sprint(tail))
	}
	if container != "" {
		q.Set("container", container)
	}
	var out string
	err := c.do(ctx, http.MethodGet, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(pod)+"/log?"+q.Encode(), nil, &out)
	return out, err
}

// CreatePod creates a single-container pod running command (image default
// entrypoint when empty). Labels are optional.
func (c *Client) CreatePod(ctx context.Context, namespace, name, image string, command []string, labels map[string]string) error {
	ctr := map[string]any{"name": "main", "image": image}
	if len(command) > 0 {
		ctr["command"] = command
	}
	return c.do(ctx, http.MethodPost, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods", map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"name": name, "labels": labels},
		"spec":     map[string]any{"restartPolicy": "Never", "containers": []any{ctr}},
	}, nil)
}

// DeletePod deletes a pod immediately; a missing pod is not an error.
func (c *Client) DeletePod(ctx context.Context, namespace, name string) error {
	err := c.do(ctx, http.MethodDelete, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(name)+"?gracePeriodSeconds=0", nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}
