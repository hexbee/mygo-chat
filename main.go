package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

//go:embed package.json
var packageJSON []byte

var CurrentAppVersion = func() string {
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(packageJSON, &pkg); err != nil || pkg.Version == "" {
		return "0.0.0"
	}
	return pkg.Version
}()

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
		Version:   CurrentAppVersion,
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

func setupMenu(win *mygo.Window) {
	isZh := strings.HasPrefix(strings.ToLower(os.Getenv("LANG")), "zh")

	checkUpdateLabel := "Check for Updates..."
	preferencesLabel := "Preferences..."
	fileLabel := "File"
	newChatLabel := "New Chat"
	searchChatsLabel := "Search Chats..."
	viewLabel := "View"
	toggleSidebarLabel := "Toggle Sidebar"

	if isZh {
		checkUpdateLabel = "检查更新..."
		preferencesLabel = "偏好设置..."
		fileLabel = "文件"
		newChatLabel = "新建对话"
		searchChatsLabel = "搜索对话..."
		viewLabel = "视图"
		toggleSidebarLabel = "切换侧边栏"
	}

	menu := mygo.NewMenu([]*mygo.MenuItem{
		{
			Role: mygo.RoleAppMenu,
			Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleAbout},
				{
					Label: checkUpdateLabel,
					Click: func(item *mygo.MenuItem, w *mygo.Window) {
						if win != nil && win.Page() != nil {
							_, _ = win.Page().Eval("window.dispatchEvent(new CustomEvent('mygo:check-update'))")
						}
					},
				},
				mygo.Separator(),
				{
					Label:       preferencesLabel,
					Accelerator: "CmdOrCtrl+,",
					Click: func(item *mygo.MenuItem, w *mygo.Window) {
						if win != nil && win.Page() != nil {
							_, _ = win.Page().Eval("window.dispatchEvent(new CustomEvent('mygo:open-settings'))")
						}
					},
				},
				mygo.Separator(),
				{Role: mygo.RoleServices},
				mygo.Separator(),
				{Role: mygo.RoleHide},
				{Role: mygo.RoleHideOthers},
				{Role: mygo.RoleUnhide},
				mygo.Separator(),
				{Role: mygo.RoleQuit},
			},
		},
		{
			Label: fileLabel,
			Submenu: []*mygo.MenuItem{
				{
					Label:       newChatLabel,
					Accelerator: "CmdOrCtrl+N",
					Click: func(item *mygo.MenuItem, w *mygo.Window) {
						if win != nil && win.Page() != nil {
							_, _ = win.Page().Eval("window.dispatchEvent(new CustomEvent('mygo:new-chat'))")
						}
					},
				},
				{
					Label:       searchChatsLabel,
					Accelerator: "CmdOrCtrl+K",
					Click: func(item *mygo.MenuItem, w *mygo.Window) {
						if win != nil && win.Page() != nil {
							_, _ = win.Page().Eval("window.dispatchEvent(new CustomEvent('mygo:search-chats'))")
						}
					},
				},
				mygo.Separator(),
				{Role: mygo.RoleClose},
			},
		},
		{Role: mygo.RoleEditMenu},
		{
			Label: viewLabel,
			Submenu: []*mygo.MenuItem{
				{
					Label:       toggleSidebarLabel,
					Accelerator: "CmdOrCtrl+B",
					Click: func(item *mygo.MenuItem, w *mygo.Window) {
						if win != nil && win.Page() != nil {
							_, _ = win.Page().Eval("window.dispatchEvent(new CustomEvent('mygo:toggle-sidebar'))")
						}
					},
				},
				mygo.Separator(),
				{Role: mygo.RoleReload},
				{Role: mygo.RoleForceReload},
				{Role: mygo.RoleToggleDevTools},
				mygo.Separator(),
				{Role: mygo.RoleResetZoom},
				{Role: mygo.RoleZoomIn},
				{Role: mygo.RoleZoomOut},
				mygo.Separator(),
				{Role: mygo.RoleToggleFullScreen},
			},
		},
		{Role: mygo.RoleWindowMenu},
	})

	mygo.App.SetMenu(menu)
}

func main() {
	mygo.Bind(AppService{})
	mygo.Bind(FileService{})
	mygo.Bind(globalUpdateService)

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Vela",
			Width:           1160,
			Height:          840,
			MinWidth:        520,
			MinHeight:       600,
			TitleBarStyle:   mygo.TitleBarHidden,
			BackgroundColor: "light-dark(#ffffff, #18181b)",
			StateKey:        "main_window",
			URL:             "/",
		})
		setupMenu(win)
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
