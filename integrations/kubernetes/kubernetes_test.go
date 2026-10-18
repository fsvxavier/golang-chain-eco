package kubernetes_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/deepagents/backends"
	"github.com/fsvxavier/golang-chain-eco/integrations/kubernetes"
)

// K8S_SERVER=https://localhost:6443 K8S_TOKEN=<sa token> [K8S_INSECURE=1] [K8S_IMAGE=debian:bookworm-slim]
// e.g. rancher/k3s in docker: kubectl create token <serviceaccount with cluster-admin>
func TestSandbox(t *testing.T) {
	server, token := os.Getenv("K8S_SERVER"), os.Getenv("K8S_TOKEN")
	if server == "" {
		t.Skip("K8S_SERVER not set")
	}
	c := &kubernetes.Client{Server: server, Token: token, Insecure: os.Getenv("K8S_INSECURE") != ""}
	image := os.Getenv("K8S_IMAGE")
	if image == "" {
		image = "debian:bookworm-slim"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	sb, err := kubernetes.NewSandbox(ctx, c, "default", image)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close(context.Background())

	pods, err := c.ListPods(ctx, "default", "app=lcg-sandbox")
	if err != nil || len(pods) == 0 || pods[0].Name != sb.Pod {
		t.Fatalf("%+v %v", pods, err)
	}

	res, err := sb.Run(ctx, "echo out; echo err >&2; exit 7", 0)
	if err != nil || res.ExitCode != 7 || !strings.Contains(res.Output, "out") || !strings.Contains(res.Output, "err") {
		t.Fatalf("%+v %v", res, err)
	}
	if res, _ = sb.Run(ctx, "echo fine", 0); res.ExitCode != 0 || strings.TrimSpace(res.Output) != "fine" {
		t.Fatalf("%+v", res)
	}
	if res, _ = sb.Run(ctx, "sleep 30", time.Second); res.ExitCode != -1 || !strings.Contains(res.Output, "timed out") {
		t.Fatalf("timeout: %+v", res)
	}

	b := sb.Backend()
	if _, err := b.Write(ctx, "/app/main.go", "package main\n// TODO: ship it\n"); err != nil {
		t.Fatal(err)
	}
	got, err := b.Read(ctx, "/app/main.go")
	if err != nil || !strings.Contains(got, "TODO") {
		t.Fatal(got, err)
	}
	if _, err := b.Read(ctx, "/missing"); err != backends.ErrNotFound {
		t.Fatal(err)
	}
	gr, err := b.Grep(ctx, "TODO", "/app", "")
	if err != nil || len(gr) != 1 || gr[0].Path != "/app/main.go" || gr[0].Line != 2 {
		t.Fatalf("%+v %v", gr, err)
	}
	if res, _ = b.Execute(ctx, "pwd", 0); strings.TrimSpace(res.Output) != "/workspace" {
		t.Fatalf("cwd: %+v", res)
	}
	logs, err := c.Logs(ctx, "default", sb.Pod, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	_ = logs
}
