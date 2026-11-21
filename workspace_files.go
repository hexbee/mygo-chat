package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// WorkspaceFileCandidate represents a file or directory match for @ mention autocomplete.
type WorkspaceFileCandidate struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // "directory" | "file"
}

const (
	defaultFileSearchMaxResults = 20
	maxFileSearchMaxResults     = 100
	defaultFileSearchMaxEntries = 50000
	fileSearchIndexTTL          = 5 * time.Second
)

var excludedSearchDirs = map[string]struct{}{
	".git":          {},
	"node_modules":  {},
	"dist":          {},
	"build":         {},
	"out":           {},
	"coverage":      {},
	"target":        {},
	".next":         {},
	".nuxt":         {},
	".turbo":        {},
	".venv":         {},
	"__pycache__":   {},
	".pytest_cache": {},
	".mypy_cache":   {},
	".gradle":       {},
}

type cachedWorkspaceIndex struct {
	entries   []WorkspaceFileCandidate
	fetchedAt time.Time
}

var (
	workspaceIndexMu    sync.Mutex
	workspaceIndexCache = map[string]cachedWorkspaceIndex{}
)

// SearchWorkspaceFiles searches files and directories under workdir for @ file mentions.
// When query is empty or contains "/", it lists direct entries of the targeted directory;
// otherwise it performs a ranked fuzzy search across the workspace.
func (WorkspaceService) SearchWorkspaceFiles(workdir string, query string, maxResults int) ([]WorkspaceFileCandidate, error) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return []WorkspaceFileCandidate{}, nil
	}
	root, err := filepath.Abs(filepath.Clean(workdir))
	if err != nil {
		return []WorkspaceFileCandidate{}, nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return []WorkspaceFileCandidate{}, nil
	}

	if maxResults <= 0 {
		maxResults = defaultFileSearchMaxResults
	} else if maxResults > maxFileSearchMaxResults {
		maxResults = maxFileSearchMaxResults
	}

	normalized := strings.ReplaceAll(query, "\\", "/")
	slash := strings.LastIndex(normalized, "/")
	if normalized == "" || slash >= 0 {
		displayDir := ""
		fragment := ""
		if slash >= 0 {
			displayDir = normalized[:slash+1]
			fragment = normalized[slash+1:]
		}
		return listWorkspaceDirectory(root, displayDir, fragment, maxResults), nil
	}

	entries := getWorkspaceIndex(root)
	visible := make([]WorkspaceFileCandidate, 0, len(entries))
	for _, entry := range entries {
		if visibleForGlobalQuery(entry.Path, normalized) {
			visible = append(visible, entry)
		}
	}
	return rankWorkspaceCandidates(visible, normalized, maxResults), nil
}

func getWorkspaceIndex(root string) []WorkspaceFileCandidate {
	now := time.Now()
	workspaceIndexMu.Lock()
	cached, ok := workspaceIndexCache[root]
	workspaceIndexMu.Unlock()
	if ok && now.Sub(cached.fetchedAt) < fileSearchIndexTTL {
		return cached.entries
	}

	entries := scanWorkspaceIndex(root)
	workspaceIndexMu.Lock()
	workspaceIndexCache[root] = cachedWorkspaceIndex{
		entries:   entries,
		fetchedAt: time.Now(),
	}
	workspaceIndexMu.Unlock()
	return entries
}

func scanWorkspaceIndex(root string) []WorkspaceFileCandidate {
	type dirItem struct {
		abs string
		rel string
	}
	queue := []dirItem{{abs: root, rel: ""}}
	indexed := make([]WorkspaceFileCandidate, 0, 256)

	for cursor := 0; cursor < len(queue) && len(indexed) < defaultFileSearchMaxEntries; cursor++ {
		dir := queue[cursor]
		entries, err := os.ReadDir(dir.abs)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})
		for _, entry := range entries {
			name := entry.Name()
			relPath := name
			if dir.rel != "" {
				relPath = dir.rel + "/" + name
			}
			if entry.IsDir() {
				if _, skip := excludedSearchDirs[name]; skip {
					continue
				}
				indexed = append(indexed, WorkspaceFileCandidate{Path: relPath, Kind: "directory"})
				queue = append(queue, dirItem{
					abs: filepath.Join(dir.abs, name),
					rel: relPath,
				})
			} else if entry.Type().IsRegular() {
				indexed = append(indexed, WorkspaceFileCandidate{Path: relPath, Kind: "file"})
			}
			if len(indexed) >= defaultFileSearchMaxEntries {
				break
			}
		}
	}
	return indexed
}

func listWorkspaceDirectory(root, displayDir, fragment string, limit int) []WorkspaceFileCandidate {
	for _, seg := range strings.Split(strings.TrimSuffix(displayDir, "/"), "/") {
		if seg == "" {
			continue
		}
		if _, skip := excludedSearchDirs[seg]; skip {
			return []WorkspaceFileCandidate{}
		}
	}
	targetAbs, ok := resolveSafeWorkspaceDir(root, displayDir)
	if !ok {
		return []WorkspaceFileCandidate{}
	}
	entries, err := os.ReadDir(targetAbs)
	if err != nil {
		return []WorkspaceFileCandidate{}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	candidates := make([]WorkspaceFileCandidate, 0, len(entries))
	showHidden := strings.HasPrefix(fragment, ".")
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !showHidden {
			continue
		}
		if entry.IsDir() {
			if _, skip := excludedSearchDirs[name]; skip {
				continue
			}
			candidates = append(candidates, WorkspaceFileCandidate{
				Path: displayDir + name,
				Kind: "directory",
			})
		} else if entry.Type().IsRegular() {
			candidates = append(candidates, WorkspaceFileCandidate{
				Path: displayDir + name,
				Kind: "file",
			})
		}
	}
	return rankWorkspaceCandidates(candidates, fragment, limit)
}

func resolveSafeWorkspaceDir(root, displayDir string) (string, bool) {
	if displayDir == "" {
		return root, true
	}
	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(displayDir)))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", false
	}
	current := root
	for _, seg := range strings.Split(rel, string(os.PathSeparator)) {
		if seg == "" || seg == "." {
			continue
		}
		current = filepath.Join(current, seg)
		st, err := os.Lstat(current)
		if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", false
		}
	}
	return target, true
}

func visibleForGlobalQuery(relPath, query string) bool {
	if strings.HasPrefix(query, ".") || strings.Contains(query, "/.") {
		return true
	}
	for _, seg := range strings.Split(relPath, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

type rankedWorkspaceCandidate struct {
	candidate WorkspaceFileCandidate
	score     int
}

func rankWorkspaceCandidates(candidates []WorkspaceFileCandidate, query string, limit int) []WorkspaceFileCandidate {
	ranked := make([]rankedWorkspaceCandidate, 0, len(candidates))
	for _, c := range candidates {
		score, ok := scoreWorkspaceCandidate(c, query)
		if ok {
			ranked = append(ranked, rankedWorkspaceCandidate{candidate: c, score: score})
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		ki, kj := kindOrder(ranked[i].candidate.Kind), kindOrder(ranked[j].candidate.Kind)
		if ki != kj {
			return ki < kj
		}
		if query != "" && len(ranked[i].candidate.Path) != len(ranked[j].candidate.Path) {
			return len(ranked[i].candidate.Path) < len(ranked[j].candidate.Path)
		}
		return ranked[i].candidate.Path < ranked[j].candidate.Path
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	result := make([]WorkspaceFileCandidate, len(ranked))
	for i, r := range ranked {
		result[i] = r.candidate
	}
	return result
}

func scoreWorkspaceCandidate(c WorkspaceFileCandidate, query string) (int, bool) {
	if query == "" {
		return 0, true
	}
	lowerPath := strings.ToLower(c.Path)
	name := lowerPath
	if idx := strings.LastIndex(lowerPath, "/"); idx >= 0 {
		name = lowerPath[idx+1:]
	}
	needle := strings.ToLower(query)
	dirBonus := 0
	if c.Kind == "directory" {
		dirBonus = 25
	}
	if name == needle {
		return 1000 + dirBonus, true
	}
	if strings.HasPrefix(name, needle) {
		return 900 + dirBonus, true
	}
	if strings.Contains(name, needle) {
		return 700 + dirBonus, true
	}
	if strings.Contains(lowerPath, needle) {
		return 500 + dirBonus, true
	}
	if sub, ok := subsequenceMatchScore(lowerPath, needle); ok {
		return 300 + sub + dirBonus, true
	}
	return 0, false
}

func subsequenceMatchScore(target, query string) (int, bool) {
	targetIdx := 0
	gap := 0
	for i := 0; i < len(query); i++ {
		found := strings.IndexByte(target[targetIdx:], query[i])
		if found < 0 {
			return 0, false
		}
		gap += found
		targetIdx += found + 1
	}
	score := 100 - gap
	if score < 0 {
		score = 0
	}
	return score, true
}

func kindOrder(kind string) int {
	if kind == "directory" {
		return 0
	}
	return 1
}
