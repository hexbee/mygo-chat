package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var (
	loginPathOnce   sync.Once
	cachedLoginPath string
)

// resolveLoginShellPATH probes the user's login shell PATH once on macOS/Linux
// so GUI-launched apps can find npx, uvx, node, python3, and user binaries.
func resolveLoginShellPATH() string {
	loginPathOnce.Do(func() {
		cachedLoginPath = computeLoginShellPATH()
	})
	return cachedLoginPath
}

func computeLoginShellPATH() string {
	basePath := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		return basePath
	}

	var parts []string
	seen := make(map[string]struct{})
	addPathList := func(raw string) {
		for _, dir := range filepath.SplitList(raw) {
			d := strings.TrimSpace(dir)
			if d == "" {
				continue
			}
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			parts = append(parts, d)
		}
	}

	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		if runtime.GOOS == "darwin" {
			shell = "/bin/zsh"
		} else {
			shell = "/bin/sh"
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-lc", `printf '%s' "$PATH"`)
	if out, err := cmd.Output(); err == nil {
		addPathList(strings.TrimSpace(string(out)))
	}

	addPathList(basePath)

	// Common macOS/Linux user tool paths as fallback when GUI launch omits them.
	home, _ := os.UserHomeDir()
	commonDirs := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/go/bin",
	}
	if home != "" {
		commonDirs = append(commonDirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".asdf", "shims"),
			filepath.Join(home, ".bun", "bin"),
		)
	}
	for _, dir := range commonDirs {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			addPathList(dir)
		}
	}

	return strings.Join(parts, string(os.PathListSeparator))
}

func lookPathInEnvPATH(file string, pathEnv string) (string, error) {
	if strings.ContainsRune(file, os.PathSeparator) || (runtime.GOOS == "windows" && strings.ContainsRune(file, '/')) {
		info, err := os.Stat(file)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", file)
		}
		if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("%s is not executable", file)
		}
		return file, nil
	}

	var exts []string
	if runtime.GOOS == "windows" {
		pathExt := os.Getenv("PATHEXT")
		if pathExt == "" {
			pathExt = ".COM;.EXE;.BAT;.CMD"
		}
		for _, ext := range strings.Split(pathExt, ";") {
			if e := strings.TrimSpace(ext); e != "" {
				exts = append(exts, strings.ToLower(e))
			}
		}
	}

	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, file)
		if runtime.GOOS == "windows" {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
			for _, ext := range exts {
				if info, err := os.Stat(candidate + ext); err == nil && !info.IsDir() {
					return candidate + ext, nil
				}
			}
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func classifyMCPRuntimeKind(command string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(command)))
	base = strings.TrimSuffix(base, ".exe")
	base = strings.TrimSuffix(base, ".cmd")
	switch base {
	case "npx", "node", "npm", "pnpm", "yarn", "bunx":
		return "npx"
	case "uvx", "uv", "python", "python3", "pipx":
		return "uvx"
	default:
		return "binary"
	}
}

// checkMCPRuntime performs a LookPath preflight against the resolved login shell PATH
// and returns actionable guidance when npx/uvx or another local executable is missing.
func checkMCPRuntime(command string) MCPRuntimeCheck {
	return checkMCPRuntimeWithPATH(command, resolveLoginShellPATH())
}

func checkMCPRuntimeWithPATH(command string, pathEnv string) MCPRuntimeCheck {
	trimmed := strings.TrimSpace(command)
	kind := classifyMCPRuntimeKind(trimmed)
	if trimmed == "" {
		return MCPRuntimeCheck{
			Found:   false,
			Command: "",
			Kind:    kind,
			Message: "Command is empty.",
		}
	}
	expanded := expandHomePath(trimmed)
	target := trimmed
	if strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, `~\`) {
		target = expanded
	}
	resolved, err := lookPathInEnvPATH(target, pathEnv)
	if err == nil {
		return MCPRuntimeCheck{
			Found:        true,
			Command:      trimmed,
			ResolvedPath: resolved,
			Kind:         kind,
		}
	}

	var msg string
	switch kind {
	case "npx":
		msg = fmt.Sprintf("Command %q was not found in PATH. Install Node.js (which includes npx), specify the executable's absolute path, or switch to a remote Streamable HTTP MCP server.", trimmed)
	case "uvx":
		msg = fmt.Sprintf("Command %q was not found in PATH. Install uv (https://docs.astral.sh/uv/), specify the executable's absolute path, or switch to a remote Streamable HTTP MCP server.", trimmed)
	default:
		msg = fmt.Sprintf("Command %q was not found in PATH. Verify it is installed, provide its absolute path, or switch to a Streamable HTTP MCP server.", trimmed)
	}
	return MCPRuntimeCheck{
		Found:   false,
		Command: trimmed,
		Kind:    kind,
		Message: msg,
	}
}

// buildMCPStdioEnv scrubs sensitive parent env vars (KEY|PASSWORD|SECRET|TOKEN),
// injects the resolved login-shell PATH, and overlays explicit server env values.
func buildMCPStdioEnv(extra map[string]string, resolvedPATH string) []string {
	clean := sanitizeEnv(os.Environ())
	envMap := make(map[string]string, len(clean)+len(extra)+1)
	for _, entry := range clean {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			envMap[k] = v
		}
	}
	if resolvedPATH != "" {
		envMap["PATH"] = resolvedPATH
	}
	for k, v := range extra {
		envMap[k] = v
	}
	out := make([]string, 0, len(envMap))
	for k, v := range envMap {
		out = append(out, k+"="+v)
	}
	return out
}
