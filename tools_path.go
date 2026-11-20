package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	errPathOutsideWorkspace = errors.New("path is outside the allowed workspace")
	errBrokenSymlink        = errors.New("broken or unresolvable symlink")
)

// scratchDirForSession returns ~/.vela/scratch/<sessionID> after strict sessionID validation.
func scratchDirForSession(sessionID string) (string, error) {
	if !validScratchSessionID.MatchString(sessionID) {
		return "", errors.New("invalid session id for scratch directory")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	scratchRoot := filepath.Join(home, ".vela", "scratch")
	target := filepath.Join(scratchRoot, sessionID)
	rel, err := filepath.Rel(scratchRoot, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("invalid scratch directory path")
	}
	return filepath.Clean(target), nil
}

// resolveRootForRequest determines the effective root directory for a tool request.
// When req.Workdir is empty and the tool is write/edit, it resolves the session's scratch directory.
// If createIfNeeded is true and the scratch directory is lazily initialized, assignedScratch is returned.
func resolveRootForRequest(req ToolExecuteRequest, createIfNeeded bool) (root string, assignedScratch string, err error) {
	if strings.TrimSpace(req.Workdir) != "" {
		cleaned := filepath.Clean(req.Workdir)
		if !filepath.IsAbs(cleaned) {
			return "", "", fmt.Errorf("workdir must be an absolute path: %q", req.Workdir)
		}
		return cleaned, "", nil
	}

	// Only write/edit lazily bind to ~/.vela/scratch/<sessionId>/ when no workdir is set.
	if req.ToolName == "write" || req.ToolName == "edit" {
		rawPath := extractTargetPath(req)
		perm := normalizePermissionMode(req.PermissionMode)
		// In full-access mode with an explicit absolute path, do not create an unnecessary scratch dir.
		if perm == PermissionFullAccess && rawPath != "" && filepath.IsAbs(rawPath) {
			return "", "", nil
		}
		scratchDir, sErr := scratchDirForSession(req.SessionID)
		if sErr != nil {
			return "", "", sErr
		}
		if createIfNeeded {
			if mkErr := os.MkdirAll(scratchDir, 0o755); mkErr != nil {
				return "", "", fmt.Errorf("failed to create scratch directory: %w", mkErr)
			}
			if canonical, cErr := filepath.EvalSymlinks(scratchDir); cErr == nil {
				return canonical, canonical, nil
			}
			return scratchDir, scratchDir, nil
		}
		return scratchDir, "", nil
	}

	return "", "", nil
}

// canonicalizePath resolves symlinks up to the nearest existing ancestor so newly created
// files and nested subdirectories are validated accurately while preventing symlink escapes.
func canonicalizePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	cur := filepath.Clean(abs)

	var tail []string
	for {
		info, lstatErr := os.Lstat(cur)
		if lstatErr == nil {
			resolved, evalErr := filepath.EvalSymlinks(cur)
			if evalErr != nil {
				if info.Mode()&os.ModeSymlink != 0 {
					return "", fmt.Errorf("%w: %s", errBrokenSymlink, cur)
				}
				return "", evalErr
			}
			for i := len(tail) - 1; i >= 0; i-- {
				seg := tail[i]
				if seg == "" || seg == "." || seg == ".." || strings.ContainsAny(seg, `/\`) {
					return "", fmt.Errorf("invalid path segment %q", seg)
				}
				resolved = filepath.Join(resolved, seg)
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(lstatErr, os.ErrNotExist) {
			return "", lstatErr
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached filesystem root without finding an existing entry.
			return filepath.Clean(abs), nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// isPathWithin reports whether canonical target is equal to or strictly inside canonical root.
func isPathWithin(canonicalRoot, canonicalTarget string) bool {
	rel, err := filepath.Rel(canonicalRoot, canonicalTarget)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

// resolveAndValidatePath resolves rawPath relative to root (if relative), canonicalizes
// symlinks via nearest existing ancestor, and checks containment against root.
// When followLeafSymlink is false (used by delete_file), only the parent directory is
// symlink-resolved so deleting a symlink inside the workspace never follows it outside.
func resolveAndValidatePath(rawPath, root string, allowOutside bool, followLeafSymlink bool) (resolved string, withinRoot bool, err error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", false, errors.New("path is required")
	}

	var candidate string
	if filepath.IsAbs(trimmed) {
		candidate = filepath.Clean(trimmed)
	} else {
		if root == "" {
			return "", false, errors.New("relative path requires an active workspace")
		}
		candidate = filepath.Join(root, trimmed)
	}

	if followLeafSymlink {
		resolved, err = canonicalizePath(candidate)
		if err != nil {
			return "", false, err
		}
	} else {
		cleaned := filepath.Clean(candidate)
		parent := filepath.Dir(cleaned)
		base := filepath.Base(cleaned)
		if base == "" || base == "." || base == ".." {
			resolved, err = canonicalizePath(cleaned)
			if err != nil {
				return "", false, err
			}
		} else {
			resolvedParent, pErr := canonicalizePath(parent)
			if pErr != nil {
				return "", false, pErr
			}
			resolved = filepath.Join(resolvedParent, base)
		}
	}

	if root == "" {
		if !allowOutside {
			return resolved, false, fmt.Errorf("%w: no active workspace", errPathOutsideWorkspace)
		}
		return resolved, false, nil
	}

	canonicalRoot, err := canonicalizePath(root)
	if err != nil {
		return "", false, err
	}

	withinRoot = isPathWithin(canonicalRoot, resolved)
	if !allowOutside && !withinRoot {
		return resolved, false, fmt.Errorf("%w: %s", errPathOutsideWorkspace, rawPath)
	}
	return resolved, withinRoot, nil
}
