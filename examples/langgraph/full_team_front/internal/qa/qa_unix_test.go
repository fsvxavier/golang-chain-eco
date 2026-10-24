//go:build unix

package qa

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A timeout must return promptly and kill the whole process group, including
// a grandchild that still holds the output pipe.
func TestCommandRunnerTimeoutKillsProcessGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("QA_HELPER", "2")
	t.Setenv("PIDFILE", pidfile)
	r := commandRunner(os.Args[0], 1<<20, 500*time.Millisecond, []string{"QA_HELPER", "PIDFILE"})
	start := time.Now()
	_, err := r(context.Background(), "http://app.test", "s")
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("returned after %s", el)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	b, rerr := os.ReadFile(pidfile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, _ := strconv.Atoi(string(b))
	for i := 0; i < 40; i++ {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("grandchild %d still alive", pid)
}
