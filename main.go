package main

import (
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
)

// AppService provides basic desktop metadata and native bridge
type AppService struct{}

type AppInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"goVersion"`
	Version   string `json:"version"`
}

func (AppService) GetAppInfo() AppInfo {
	return AppInfo{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		GoVersion: runtime.Version(),
		Version:   "0.1.0",
	}
}

// FileService handles native file dialogs and file operations
type FileService struct{}

type FileOpenResult struct {
	Name    string `json:"name"`
	Content string `json:"content"` // base64
}

// SaveFile prompts the user with a native Save dialog and writes the base64 content
func (FileService) SaveFile(defaultName string, contentBase64 string) (bool, error) {
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title:       "导出历史记录",
		DefaultPath: defaultName,
		Filters: []mygo.FileFilter{
			{Name: "ZIP 归档", Extensions: []string{"zip"}},
		},
	})
	if err != nil || path == "" {
		return false, err
	}
	data, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return false, err
	}
	return true, nil
}

// OpenFile prompts the user to select a zip file and returns its content as base64
func (FileService) OpenFile() (*FileOpenResult, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title: "导入历史记录",
		Filters: []mygo.FileFilter{
			{Name: "ZIP 归档", Extensions: []string{"zip"}},
		},
	})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	filePath := paths[0]
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return &FileOpenResult{
		Name:    filepath.Base(filePath),
		Content: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func main() {
	mygo.Bind(AppService{})
	mygo.Bind(FileService{})

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:           "DeepSeek Chat",
			Width:           1160,
			Height:          840,
			MinWidth:        520,
			MinHeight:       600,
			TitleBarStyle:   mygo.TitleBarHidden,
			BackgroundColor: "light-dark(#ffffff, #18181b)",
			StateKey:        "main_window",
			URL:             "/",
		})
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
