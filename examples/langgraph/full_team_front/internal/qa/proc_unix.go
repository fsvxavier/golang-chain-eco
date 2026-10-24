//go:build unix

package qa

import (
	"os/exec"
	"syscall"
	"time"
)

// killGroup puts the child in its own process group and kills the whole group
// on cancel, so grandchildren (node, chromium) die too; WaitDelay bounds the
// wait for pipes they might still hold.
func killGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 5 * time.Second
}
