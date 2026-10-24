//go:build !unix

package qa

import (
	"os/exec"
	"time"
)

// killGroup: no process groups here; only the direct child is killed.
func killGroup(c *exec.Cmd) { c.WaitDelay = 5 * time.Second }
