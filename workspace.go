package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/egoist/mygo"
)

var validScratchSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// WorkspaceService handles native directory picking, scratch directory cleanup, and file manager actions.
type WorkspaceService struct{}

// PickDirectory prompts the user with a native directory chooser and returns the selected absolute path.
// Returns an empty string when the user cancels the dialog.
func (WorkspaceService) PickDirectory() (string, error) {
	title := "Select Workspace"
	if strings.HasPrefix(strings.ToLower(os.Getenv("LANG")), "zh") {
		title = "选择工作区目录"
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:     title,
		Directory: true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return filepath.Clean(paths[0]), nil
}

// RemoveScratchDir removes the isolated scratch directory (~/.vela/scratch/<sessionID>) for a session.
func (WorkspaceService) RemoveScratchDir(sessionID string) error {
	if !validScratchSessionID.MatchString(sessionID) {
		return errors.New("invalid session id")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	scratchRoot := filepath.Join(home, ".vela", "scratch")
	target := filepath.Join(scratchRoot, sessionID)
	rel, err := filepath.Rel(scratchRoot, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("invalid scratch directory path")
	}
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(target)
}

// OpenPath opens a file or directory using the system default application.
func (WorkspaceService) OpenPath(absPath string) error {
	cleaned := filepath.Clean(absPath)
	if !filepath.IsAbs(cleaned) {
		return errors.New("path must be absolute")
	}
	return mygo.Shell.OpenPath(cleaned)
}

// RevealInFinder reveals the specified path in Finder (macOS), Explorer (Windows), or the file manager (Linux).
func (WorkspaceService) RevealInFinder(absPath string) error {
	cleaned := filepath.Clean(absPath)
	if !filepath.IsAbs(cleaned) {
		return errors.New("path must be absolute")
	}
	mygo.Shell.ShowItemInFolder(cleaned)
	return nil
}
