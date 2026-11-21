//go:build !darwin && !linux

package main

import "os/exec"

func isOSSandboxAvailable() bool {
	return false
}

func buildSandboxedShellCommand(command string, _ sandboxSpec) (*exec.Cmd, bool) {
	return buildShellCommand(command), false
}
