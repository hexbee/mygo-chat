//go:build windows

package main

import (
	"os/exec"
	"strconv"
)

func buildShellCommand(command string) *exec.Cmd {
	shell := "pwsh"
	if _, err := exec.LookPath(shell); err != nil {
		shell = "powershell.exe"
	}
	return exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", command)
}

func configureProcessGroup(_ *exec.Cmd) {
	// Process tree termination on Windows is handled via taskkill /T /F.
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pid <= 0 {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
}
