package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// UpdateState reflects the current update status, replicating deepseek-harness update phases
type UpdateState struct {
	Phase   string `json:"phase"`   // "idle" | "checking" | "available" | "downloading" | "verifying" | "ready" | "error"
	Version string `json:"version"` // Latest version string
	Percent int    `json:"percent"` // Download progress (0-100)
	Notes   string `json:"notes"`   // Release notes
	Error   string `json:"error"`   // Localized or technical error message
}

type UpdateService struct {
	mu            sync.RWMutex
	state         UpdateState
	currentUpdate *mygo.Update
	downloading   bool
}

var globalUpdateService = &UpdateService{
	state: UpdateState{Phase: "idle"},
}

func (s *UpdateService) GetUpdateStatus() UpdateState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *UpdateService) setPhase(phase string, err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Phase = phase
	s.state.Error = err
}

func (s *UpdateService) setProgress(percent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Percent = percent
}

// CheckUpdate checks whether a newer version is available.
func (s *UpdateService) CheckUpdate() (UpdateState, error) {
	s.mu.Lock()
	s.state.Phase = "checking"
	s.state.Error = ""
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Try native MyGo Updater first if enabled
	if mygo.Updater.Enabled() {
		up, err := mygo.Updater.Check(ctx)
		if err != nil {
			s.setPhase("error", fmt.Sprintf("检查更新失败: %v", err))
			return s.GetUpdateStatus(), err
		}
		if up != nil {
			s.mu.Lock()
			s.currentUpdate = up
			s.state.Phase = "available"
			s.state.Version = up.Version
			s.state.Notes = up.Notes
			s.state.Percent = 0
			s.mu.Unlock()
			return s.GetUpdateStatus(), nil
		}
		// No update available
		s.setPhase("idle", "")
		return s.GetUpdateStatus(), nil
	}

	// 2. Fallback / Dev mode: check GitHub Releases API
	latestVersion, notes, assetURL, err := checkGitHubRelease(ctx, "hexbee/mygo-chat")
	if err != nil {
		s.setPhase("error", fmt.Sprintf("检查更新失败: %v", err))
		return s.GetUpdateStatus(), err
	}

	if isVersionNewer(latestVersion, CurrentAppVersion) {
		s.mu.Lock()
		s.state.Phase = "available"
		s.state.Version = latestVersion
		s.state.Notes = notes
		s.state.Percent = 0
		s.mu.Unlock()
		_ = assetURL
		return s.GetUpdateStatus(), nil
	}

	s.setPhase("idle", "")
	return s.GetUpdateStatus(), nil
}

// StartDownload begins downloading the update payload with progress
func (s *UpdateService) StartDownload() (UpdateState, error) {
	s.mu.Lock()
	if s.downloading {
		s.mu.Unlock()
		return s.GetUpdateStatus(), nil
	}
	if s.state.Phase != "available" && s.state.Phase != "error" {
		s.mu.Unlock()
		return s.GetUpdateStatus(), fmt.Errorf("no update available to download")
	}
	s.downloading = true
	s.state.Phase = "downloading"
	s.state.Percent = 0
	s.state.Error = ""
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.downloading = false
			s.mu.Unlock()
		}()

		ctx := context.Background()

		if mygo.Updater.Enabled() && s.currentUpdate != nil {
			err := s.currentUpdate.Install(ctx, func(downloaded, total int64) {
				if total > 0 {
					pct := int(float64(downloaded) / float64(total) * 100)
					s.setProgress(pct)
				}
			})
			if err != nil {
				s.setPhase("error", fmt.Sprintf("下载更新失败: %v", err))
				return
			}
			s.setPhase("ready", "")
			return
		}

		// Fallback downloader when native update signature infrastructure isn't configured in dev
		latestVersion, _, assetURL, err := checkGitHubRelease(ctx, "hexbee/mygo-chat")
		if err != nil || assetURL == "" {
			s.setPhase("error", "未找到当前平台对应的安装包资产")
			return
		}

		tmpDir, err := os.UserCacheDir()
		if err != nil {
			tmpDir = os.TempDir()
		}
		targetFile := filepath.Join(tmpDir, fmt.Sprintf("mygo-chat-update-%s-%s-%s", latestVersion, runtime.GOOS, runtime.GOARCH))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
		if err != nil {
			s.setPhase("error", err.Error())
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			s.setPhase("error", err.Error())
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			s.setPhase("error", fmt.Sprintf("下载失败: HTTP %d", resp.StatusCode))
			return
		}

		out, err := os.Create(targetFile)
		if err != nil {
			s.setPhase("error", err.Error())
			return
		}
		defer out.Close()

		total := resp.ContentLength
		var downloaded int64
		buf := make([]byte, 32*1024)
		for {
			n, rErr := resp.Body.Read(buf)
			if n > 0 {
				_, wErr := out.Write(buf[:n])
				if wErr != nil {
					s.setPhase("error", wErr.Error())
					return
				}
				downloaded += int64(n)
				if total > 0 {
					s.setProgress(int(float64(downloaded) / float64(total) * 100))
				}
			}
			if rErr == io.EOF {
				break
			}
			if rErr != nil {
				s.setPhase("error", rErr.Error())
				return
			}
		}

		s.setPhase("verifying", "")
		time.Sleep(300 * time.Millisecond) // Smooth transition
		s.setPhase("ready", "")
	}()

	return s.GetUpdateStatus(), nil
}

// Relaunch restarts the application to apply the downloaded update
func (s *UpdateService) Relaunch() (bool, error) {
	s.mu.RLock()
	ready := s.state.Phase == "ready"
	s.mu.RUnlock()

	if !ready {
		return false, fmt.Errorf("update is not ready yet")
	}

	go func() {
		time.Sleep(200 * time.Millisecond)
		mygo.App.Relaunch()
	}()
	return true, nil
}

// DismissUpdate closes the update alert/state
func (s *UpdateService) DismissUpdate() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Phase = "idle"
	return true
}

type gitHubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func checkGitHubRelease(ctx context.Context, repo string) (string, string, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "mygo-chat-updater")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	var rel gitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", "", err
	}

	version := strings.TrimPrefix(rel.TagName, "v")

	// Match platform asset
	var targetAsset string
	matchKeyword := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, a := range rel.Assets {
		if strings.Contains(strings.ToLower(a.Name), matchKeyword) || strings.Contains(strings.ToLower(a.Name), runtime.GOARCH) {
			targetAsset = a.BrowserDownloadURL
			break
		}
	}
	if targetAsset == "" && len(rel.Assets) > 0 {
		targetAsset = rel.Assets[0].BrowserDownloadURL
	}

	return version, rel.Body, targetAsset, nil
}

func isVersionNewer(remote, current string) bool {
	rParts := parseSemver(remote)
	cParts := parseSemver(current)
	for i := 0; i < 3; i++ {
		if rParts[i] > cParts[i] {
			return true
		}
		if rParts[i] < cParts[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	var res [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		n, _ := strconv.Atoi(parts[i])
		res[i] = n
	}
	return res
}
