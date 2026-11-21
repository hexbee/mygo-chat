//go:build windows

package main

// mcpProcessAlive is not implemented on Windows: the test harness skips
// process-tree assertions there because there is no portable signal-0 probe.
func mcpProcessAlive(int) bool { return false }

// spawnMCPHelperGrandchild is a no-op on Windows; the helper simply records 0.
func spawnMCPHelperGrandchild() int { return 0 }
