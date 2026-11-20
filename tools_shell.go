package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	bashDefaultTimeoutMs = 60_000
	bashMaxTimeoutMs     = 300_000
	bashMaxOutputBytes   = 64 * 1024
)

var sensitiveEnvPattern = regexp.MustCompile(`(?i)(KEY|PASSWORD|SECRET|TOKEN)`)

// sanitizeEnv removes parent environment variables matching KEY|PASSWORD|SECRET|TOKEN
// before launching child shell processes.
func sanitizeEnv(environ []string) []string {
	clean := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, hasEq := strings.Cut(entry, "=")
		if !hasEq {
			continue
		}
		if sensitiveEnvPattern.MatchString(key) {
			continue
		}
		clean = append(clean, entry)
	}
	return clean
}

type boundedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	origLen := len(p)
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return origLen, nil
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.truncated = true
		return origLen, nil
	}
	b.buf.Write(p)
	return origLen, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

type fileStamp struct {
	size        int64
	modTimeNano int64
}

func snapshotDirFiles(dir string) map[string]fileStamp {
	snap := make(map[string]fileStamp)
	if dir == "" {
		return snap
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, rErr := filepath.Rel(dir, path)
		if rErr != nil {
			return nil
		}
		if info, iErr := d.Info(); iErr == nil {
			snap[rel] = fileStamp{
				size:        info.Size(),
				modTimeNano: info.ModTime().UnixNano(),
			}
		}
		return nil
	})
	return snap
}

func diffSnapshots(dir string, before, after map[string]fileStamp) []string {
	var changed []string
	for rel, stamp := range after {
		prev, existed := before[rel]
		if !existed || prev.size != stamp.size || prev.modTimeNano != stamp.modTimeNano {
			changed = append(changed, filepath.Join(dir, rel))
		}
	}
	sort.Strings(changed)
	return changed
}

func isDirEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

// executeBash implements bash(description?, command, workdir?, timeoutMs?) with:
//   - process-group isolation and full subtree cleanup on cancel/timeout
//   - sensitive environment variable stripping
//   - lazy scratch dir creation with post-exit empty-dir cleanup
//   - before/after file snapshot diff for uiMeta.producedFiles
//   - non-zero exit codes returned as normal (non-error) tool results
func executeBash(ctx context.Context, req ToolExecuteRequest) (ToolExecuteResult, error) {
	command := strings.TrimSpace(stringInput(req.Input, "command"))
	if command == "" {
		return toolErrorResult("Error: command is required", req.Workdir)
	}

	timeoutMs := intInput(req.Input, "timeoutMs", bashDefaultTimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = bashDefaultTimeoutMs
	}
	if timeoutMs > bashMaxTimeoutMs {
		timeoutMs = bashMaxTimeoutMs
	}

	inputWorkdir := strings.TrimSpace(stringInput(req.Input, "workdir"))
	reqWorkdir := strings.TrimSpace(req.Workdir)

	var cwd string
	createdScratchNow := false

	switch {
	case inputWorkdir != "":
		resolved, _, err := resolveAndValidatePath(inputWorkdir, reqWorkdir, true, true)
		if err != nil {
			return toolErrorResult("Error: "+err.Error(), reqWorkdir)
		}
		cwd = resolved
	case reqWorkdir != "":
		resolved, err := canonicalizePath(reqWorkdir)
		if err != nil {
			return toolErrorResult("Error: "+err.Error(), reqWorkdir)
		}
		cwd = resolved
	default:
		// No workdir bound yet: lazily prepare ~/.vela/scratch/<sessionId>/ for safe cwd.
		scratchDir, err := scratchDirForSession(req.SessionID)
		if err != nil {
			return toolErrorResult("Error: "+err.Error(), "")
		}
		if _, statErr := os.Lstat(scratchDir); os.IsNotExist(statErr) {
			createdScratchNow = true
		}
		if err := os.MkdirAll(scratchDir, 0o755); err != nil {
			return toolErrorResult(fmt.Sprintf("Error creating scratch directory: %v", err), "")
		}
		if canonical, cErr := filepath.EvalSymlinks(scratchDir); cErr == nil {
			cwd = canonical
		} else {
			cwd = scratchDir
		}
	}

	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return toolErrorResult(fmt.Sprintf("Error: workdir %q does not exist or is not a directory", cwd), reqWorkdir)
	}

	beforeSnap := snapshotDirFiles(cwd)

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	cmd := buildShellCommand(command)
	cmd.Dir = cwd
	cmd.Env = sanitizeEnv(os.Environ())
	configureProcessGroup(cmd)

	stdoutBuf := newBoundedBuffer(bashMaxOutputBytes)
	stderrBuf := newBoundedBuffer(bashMaxOutputBytes)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		if createdScratchNow && isDirEmpty(cwd) {
			_ = os.Remove(cwd)
		}
		return toolErrorResult(fmt.Sprintf("Error starting command: %v", err), reqWorkdir)
	}

	doneCh := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			killProcessTree(cmd)
		case <-doneCh:
		}
	}()

	waitErr := cmd.Wait()
	close(doneCh)

	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	canceled := errors.Is(ctx.Err(), context.Canceled)

	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	// Post-exit scratch cleanup & snapshot diff:
	// Only remove cwd if it was newly lazily created for this call AND remains completely empty.
	assignedScratch := ""
	var producedFiles []string
	effectiveWorkdir := cwd
	if createdScratchNow && isDirEmpty(cwd) {
		_ = os.Remove(cwd)
		effectiveWorkdir = ""
	} else {
		if createdScratchNow {
			assignedScratch = cwd
		}
		afterSnap := snapshotDirFiles(cwd)
		producedFiles = diffSnapshots(cwd, beforeSnap, afterSnap)
	}

	stdoutStr := strings.TrimRight(stdoutBuf.String(), "\r\n")
	stderrStr := strings.TrimRight(stderrBuf.String(), "\r\n")
	truncated := stdoutBuf.Truncated() || stderrBuf.Truncated()

	var out strings.Builder
	if stdoutStr != "" {
		out.WriteString(stdoutStr)
	}
	if stderrStr != "" {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString("[stderr]\n")
		out.WriteString(stderrStr)
	}
	if truncated {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(fmt.Sprintf("[truncated: output exceeded %d KB limit]", bashMaxOutputBytes/1024))
	}
	if timedOut {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(fmt.Sprintf("[timed out after %dms]", timeoutMs))
	} else if canceled {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString("[cancelled by user]")
	}
	if exitCode != 0 {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(fmt.Sprintf("[exit code: %d]", exitCode))
	}
	if out.Len() == 0 {
		out.WriteString("(no output)")
	}

	meta := map[string]any{
		"exitCode":  exitCode,
		"truncated": truncated,
	}
	if effectiveWorkdir != "" {
		meta["workdir"] = effectiveWorkdir
	}
	if len(producedFiles) > 0 {
		meta["producedFiles"] = producedFiles
	}

	return ToolExecuteResult{
		ModelContent:    out.String(),
		IsError:         canceled,
		AssignedScratch: assignedScratch,
		UIMeta:          meta,
	}, nil
}
