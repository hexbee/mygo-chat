package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const trashDirName = ".vela-trash"

// moveToTrash safely moves targetPath into the workspace's .vela-trash/ directory
// (or ~/.vela/trash/ when no workdir is bound) using os.Lstat so symlinks are never followed.
func moveToTrash(targetPath, workdir string) (string, error) {
	info, err := os.Lstat(targetPath)
	if err != nil {
		return "", err
	}

	if targetPath == filepath.Dir(targetPath) {
		return "", errors.New("refusing to delete filesystem root")
	}

	var trashRoot string
	if workdir != "" {
		canonicalRoot, cErr := canonicalizePath(workdir)
		if cErr != nil {
			return "", cErr
		}
		if targetPath == canonicalRoot {
			return "", errors.New("refusing to delete workspace root directory")
		}
		trashRoot = filepath.Join(canonicalRoot, trashDirName)
	} else {
		home, hErr := os.UserHomeDir()
		if hErr != nil {
			trashRoot = filepath.Join(filepath.Dir(targetPath), trashDirName)
		} else {
			trashRoot = filepath.Join(home, ".vela", "trash")
		}
	}

	if err := os.MkdirAll(trashRoot, 0o755); err != nil {
		return "", fmt.Errorf("failed to create trash directory: %w", err)
	}

	stamp := time.Now().Format("20060102-150405")
	suffix := randomHex(4)
	destName := fmt.Sprintf("%s-%s-%s", stamp, suffix, filepath.Base(targetPath))
	destPath := filepath.Join(trashRoot, destName)

	if err := os.Rename(targetPath, destPath); err == nil {
		return destPath, nil
	} else if workdir != "" {
		// Inside the same workspace, rename should succeed; surface error if not.
		return "", fmt.Errorf("failed to move %q to trash: %w", targetPath, err)
	}

	// Cross-device fallback (only possible when workdir == "" in full-access mode).
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, rErr := os.Readlink(targetPath)
		if rErr != nil {
			return "", rErr
		}
		if sErr := os.Symlink(linkTarget, destPath); sErr != nil {
			return "", sErr
		}
		if rmErr := os.Remove(targetPath); rmErr != nil {
			_ = os.Remove(destPath)
			return "", rmErr
		}
		return destPath, nil
	}

	if info.IsDir() {
		// Fall back to a sibling .vela-trash directory on the same device to keep rename atomic.
		localTrash := filepath.Join(filepath.Dir(targetPath), trashDirName)
		if mkErr := os.MkdirAll(localTrash, 0o755); mkErr != nil {
			return "", mkErr
		}
		destPath = filepath.Join(localTrash, destName)
		if renErr := os.Rename(targetPath, destPath); renErr != nil {
			return "", renErr
		}
		return destPath, nil
	}

	if cErr := copyFileTo(targetPath, destPath, info.Mode().Perm()); cErr != nil {
		return "", cErr
	}
	if rmErr := os.Remove(targetPath); rmErr != nil {
		_ = os.Remove(destPath)
		return "", rmErr
	}
	return destPath, nil
}

func copyFileTo(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
