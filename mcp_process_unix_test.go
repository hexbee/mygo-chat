//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"syscall"
)

// mcpProcessAlive reports whether pid still exists (including as a zombie), which
// is exactly the leak the stdio teardown must avoid.
func mcpProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// spawnMCPHelperGrandchild starts a long-lived grandchild in the helper's process
// group so tests can prove the whole tree dies when the generation closes. The
// child's stdio is detached so it cannot keep the MCP pipes open.
func spawnMCPHelperGrandchild() int {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		return 0
	}
	pid := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	return pid
}
