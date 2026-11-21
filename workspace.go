package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo"
)

var validScratchSessionID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

const (
	previewMaxTextBytes  = 512 * 1024
	previewMaxImageBytes = 10 * 1024 * 1024
)

// FilePreview carries structured metadata and preview content for a produced file.
type FilePreview struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Ext       string `json:"ext"`
	Size      int64  `json:"size"`
	Kind      string `json:"kind"` // "html" | "markdown" | "image" | "code" | "text" | "binary"
	MimeType  string `json:"mimeType"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

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

// SavePathAs opens a native Save dialog and copies the file at absPath to the user-selected destination.
// Returns the destination path, or an empty string if the user cancelled.
func (WorkspaceService) SavePathAs(absPath string) (string, error) {
	cleaned := filepath.Clean(absPath)
	if !filepath.IsAbs(cleaned) {
		return "", errors.New("path must be absolute")
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("cannot save a directory as a single file")
	}

	title := "Save As"
	if strings.HasPrefix(strings.ToLower(os.Getenv("LANG")), "zh") {
		title = "另存为"
	}

	opts := mygo.SaveDialogOptions{
		Title:       title,
		DefaultPath: filepath.Base(cleaned),
	}
	if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(cleaned)), "."); ext != "" {
		opts.Filters = []mygo.FileFilter{
			{Name: strings.ToUpper(ext) + " File", Extensions: []string{ext}},
		}
	}

	destPath, err := mygo.Dialog.Save(opts)
	if err != nil || destPath == "" {
		return "", err
	}

	if err := copyFileAtomic(cleaned, destPath, info.Mode().Perm()); err != nil {
		return "", err
	}
	return destPath, nil
}

func copyFileAtomic(srcPath, destPath string, perm os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(destPath), ".vela-save-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if perm == 0 {
		perm = 0o644
	}
	_ = os.Chmod(tmpPath, perm)
	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// ReadFilePreview inspects a local file and returns a bounded preview payload (HTML, Markdown, Image, Code, Text, or Binary).
func (WorkspaceService) ReadFilePreview(absPath string) (*FilePreview, error) {
	cleaned := filepath.Clean(absPath)
	if !filepath.IsAbs(cleaned) {
		return nil, errors.New("path must be absolute")
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("cannot preview a directory")
	}

	name := filepath.Base(cleaned)
	ext := strings.ToLower(filepath.Ext(name))
	mimeType := mime.TypeByExtension(ext)

	preview := &FilePreview{
		Path:     cleaned,
		Name:     name,
		Ext:      ext,
		Size:     info.Size(),
		MimeType: mimeType,
	}

	if isImagePreviewExt(ext) {
		preview.Kind = "image"
		if preview.MimeType == "" {
			preview.MimeType = imageMimeFallback(ext)
		}
		if info.Size() > previewMaxImageBytes {
			preview.Truncated = true
			return preview, nil
		}
		data, rErr := os.ReadFile(cleaned)
		if rErr != nil {
			return nil, rErr
		}
		preview.Content = fmt.Sprintf("data:%s;base64,%s", preview.MimeType, base64.StdEncoding.EncodeToString(data))
		return preview, nil
	}

	f, err := os.Open(cleaned)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	readLimit := int64(previewMaxTextBytes)
	truncated := info.Size() > readLimit
	data, err := io.ReadAll(io.LimitReader(f, readLimit))
	if err != nil {
		return nil, err
	}

	if preview.MimeType == "" && len(data) > 0 {
		preview.MimeType = http.DetectContentType(data)
	}

	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		preview.Kind = "binary"
		preview.Truncated = truncated
		return preview, nil
	}

	preview.Content = string(data)
	preview.Truncated = truncated

	switch ext {
	case ".html", ".htm":
		preview.Kind = "html"
		if preview.MimeType == "" {
			preview.MimeType = "text/html"
		}
	case ".md", ".markdown", ".mdx":
		preview.Kind = "markdown"
		if preview.MimeType == "" {
			preview.MimeType = "text/markdown"
		}
	default:
		if isCodePreviewExt(ext) {
			preview.Kind = "code"
		} else {
			preview.Kind = "text"
		}
		if preview.MimeType == "" {
			preview.MimeType = "text/plain"
		}
	}
	return preview, nil
}

func isImagePreviewExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico", ".avif":
		return true
	default:
		return false
	}
}

func imageMimeFallback(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	case ".avif":
		return "image/avif"
	default:
		return "application/octet-stream"
	}
}

func isCodePreviewExt(ext string) bool {
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".rs", ".java", ".kt", ".swift",
		".c", ".h", ".cpp", ".hpp", ".cs", ".rb", ".php", ".sh", ".bash", ".zsh", ".ps1",
		".css", ".scss", ".less", ".json", ".yaml", ".yml", ".toml", ".xml", ".sql", ".graphql",
		".dockerfile", ".makefile", ".ini", ".env", ".csv":
		return true
	default:
		return false
	}
}
