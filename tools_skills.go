package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	skillScopeProject = "project"
	skillScopeUser    = "user"
	maxSkillFileBytes = 256 * 1024
)

var (
	defaultSkillProjectDirs = []string{".agents/skills", ".vela/skills"}
	defaultSkillUserDirs    = []string{"~/.agents/skills", "~/.vela/skills"}
)

// SkillConfig controls skill discovery directories, precedence order, and per-skill enablement.
type SkillConfig struct {
	ProjectDirs    []string `json:"projectDirs,omitempty"`
	UserDirs       []string `json:"userDirs,omitempty"`
	DisabledSkills []string `json:"disabledSkills,omitempty"`
}

// SkillSummary describes a discovered skill for progressive catalog disclosure.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"` // "project" | "user"
	Path        string `json:"path"`
	BaseDir     string `json:"baseDir"`
}

// SkillCatalog contains all active (enabled) discovered skills and a stable content digest.
type SkillCatalog struct {
	Skills []SkillSummary `json:"skills"`
	Digest string         `json:"digest"`
}

// SkillDocument contains the full instructions of a loaded skill.
type SkillDocument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	Path        string `json:"path"`
	BaseDir     string `json:"baseDir"`
	Content     string `json:"content"`
}

// SkillDirEntry describes one skill discovered inside a specific configured directory.
type SkillDirEntry struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Path         string `json:"path"`
	BaseDir      string `json:"baseDir"`
	Enabled      bool   `json:"enabled"`
	OverriddenBy string `json:"overriddenBy,omitempty"`
}

// SkillDirReport describes the scan result of one configured skill directory.
type SkillDirReport struct {
	Scope        string          `json:"scope"` // "project" | "user"
	Configured   string          `json:"configured"`
	ResolvedPath string          `json:"resolvedPath"`
	Exists       bool            `json:"exists"`
	Skills       []SkillDirEntry `json:"skills"`
}

// SkillInspection is the full directory-by-directory view used by the Settings UI.
type SkillInspection struct {
	Directories []SkillDirReport `json:"directories"`
	ActiveCount int              `json:"activeCount"`
	TotalCount  int              `json:"totalCount"`
}

// SkillService discovers, inspects, and loads local SKILL.md files according to SkillConfig.
type SkillService struct{}

// ListSkills scans configured project and user skill directories in priority order,
// returning enabled skills and a stable digest.
func (SkillService) ListSkills(workdir string, cfg *SkillConfig) (SkillCatalog, error) {
	return discoverSkillsWithConfig(workdir, cfg), nil
}

// ReadSkill loads the full markdown body of an enabled skill by name.
func (SkillService) ReadSkill(name string, workdir string, cfg *SkillConfig) (SkillDocument, error) {
	return loadSkillByNameWithConfig(name, workdir, cfg)
}

// InspectSkills returns a directory-by-directory breakdown of all discovered skills,
// including shadowed/overridden and disabled entries, for management in Settings.
func (SkillService) InspectSkills(workdir string, cfg *SkillConfig) (SkillInspection, error) {
	return inspectSkillsWithConfig(workdir, cfg), nil
}

type resolvedSkillRoot struct {
	scope        string
	configured   string
	resolvedPath string
}

func expandHomePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if trimmed == "~" {
				return filepath.Clean(home)
			}
			return filepath.Clean(filepath.Join(home, trimmed[2:]))
		}
	}
	return filepath.Clean(trimmed)
}

func effectiveSkillConfig(cfg *SkillConfig) (projectDirs, userDirs []string, disabled map[string]bool) {
	if cfg == nil || cfg.ProjectDirs == nil {
		projectDirs = defaultSkillProjectDirs
	} else {
		projectDirs = cfg.ProjectDirs
	}
	if cfg == nil || cfg.UserDirs == nil {
		userDirs = defaultSkillUserDirs
	} else {
		userDirs = cfg.UserDirs
	}
	disabled = make(map[string]bool)
	if cfg != nil {
		for _, name := range cfg.DisabledSkills {
			if key := strings.ToLower(strings.TrimSpace(name)); key != "" {
				disabled[key] = true
			}
		}
	}
	return projectDirs, userDirs, disabled
}

func resolveConfiguredSkillRoots(workdir string, cfg *SkillConfig) []resolvedSkillRoot {
	projectDirs, userDirs, _ := effectiveSkillConfig(cfg)
	cleanWorkdir := strings.TrimSpace(workdir)
	if cleanWorkdir != "" {
		cleanWorkdir = filepath.Clean(cleanWorkdir)
	}

	roots := make([]resolvedSkillRoot, 0, len(projectDirs)+len(userDirs))
	for _, raw := range projectDirs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		resolved := ""
		if cleanWorkdir != "" {
			expanded := expandHomePath(trimmed)
			if filepath.IsAbs(expanded) {
				resolved = expanded
			} else {
				resolved = filepath.Clean(filepath.Join(cleanWorkdir, trimmed))
			}
		}
		roots = append(roots, resolvedSkillRoot{
			scope:        skillScopeProject,
			configured:   trimmed,
			resolvedPath: resolved,
		})
	}

	for _, raw := range userDirs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		roots = append(roots, resolvedSkillRoot{
			scope:        skillScopeUser,
			configured:   trimmed,
			resolvedPath: expandHomePath(trimmed),
		})
	}
	return roots
}

func scanSkillRootDir(dir string, scope string) (exists bool, found []SkillSummary) {
	if strings.TrimSpace(dir) == "" {
		return false, nil
	}
	dirInfo, err := os.Stat(dir)
	if err != nil || !dirInfo.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true, nil
	}
	for _, entry := range entries {
		entryName := entry.Name()
		if strings.HasPrefix(entryName, ".") {
			continue
		}
		fullPath := filepath.Join(dir, entryName)
		// Use os.Stat (follows symlinks) so symlinked skill directories and .md files are discovered.
		entryInfo, statErr := os.Stat(fullPath)
		if statErr != nil {
			continue
		}

		var skillFile string
		var baseDir string
		var fallbackName string

		if entryInfo.IsDir() {
			candidate := filepath.Join(fullPath, "SKILL.md")
			if info, cErr := os.Stat(candidate); cErr == nil && !info.IsDir() {
				skillFile = candidate
				baseDir = fullPath
				fallbackName = entryName
			}
		} else if strings.HasSuffix(strings.ToLower(entryName), ".md") {
			skillFile = fullPath
			baseDir = dir
			fallbackName = strings.TrimSuffix(entryName, filepath.Ext(entryName))
		}

		if skillFile == "" {
			continue
		}

		data, readErr := readFileBounded(skillFile, maxSkillFileBytes)
		if readErr != nil {
			continue
		}
		fm, _ := parseSkillFrontmatter(string(data))
		name := strings.TrimSpace(fm.name)
		if name == "" {
			name = fallbackName
		}
		found = append(found, SkillSummary{
			Name:        name,
			Description: strings.TrimSpace(fm.description),
			Scope:       scope,
			Path:        skillFile,
			BaseDir:     baseDir,
		})
	}
	sort.Slice(found, func(i, j int) bool {
		return strings.ToLower(found[i].Name) < strings.ToLower(found[j].Name)
	})
	return true, found
}

func discoverSkillsWithConfig(workdir string, cfg *SkillConfig) SkillCatalog {
	_, _, disabled := effectiveSkillConfig(cfg)
	byName := make(map[string]SkillSummary)
	var ordered []SkillSummary

	for _, root := range resolveConfiguredSkillRoots(workdir, cfg) {
		if root.resolvedPath == "" {
			continue
		}
		_, found := scanSkillRootDir(root.resolvedPath, root.scope)
		for _, s := range found {
			key := strings.ToLower(s.Name)
			// Earlier directories have higher priority and win on name collisions.
			if _, exists := byName[key]; exists {
				continue
			}
			byName[key] = s
			if disabled[key] {
				continue
			}
			ordered = append(ordered, s)
		}
	}

	h := sha256.New()
	for _, s := range ordered {
		_, _ = fmt.Fprintf(h, "%s|%s|%s|%s\n", s.Scope, s.Name, s.Description, s.Path)
	}
	digest := ""
	if len(ordered) > 0 {
		digest = hex.EncodeToString(h.Sum(nil))[:16]
	}

	return SkillCatalog{
		Skills: ordered,
		Digest: digest,
	}
}

func inspectSkillsWithConfig(workdir string, cfg *SkillConfig) SkillInspection {
	_, _, disabled := effectiveSkillConfig(cfg)
	roots := resolveConfiguredSkillRoots(workdir, cfg)

	winnerByName := make(map[string]string) // lower(name) -> configured dir label
	reports := make([]SkillDirReport, 0, len(roots))
	activeCount := 0
	totalCount := 0

	for _, root := range roots {
		exists, found := scanSkillRootDir(root.resolvedPath, root.scope)
		entries := make([]SkillDirEntry, 0, len(found))
		for _, s := range found {
			totalCount++
			key := strings.ToLower(s.Name)
			enabled := !disabled[key]
			overriddenBy := ""
			if prev, exists := winnerByName[key]; exists {
				overriddenBy = prev
			} else {
				winnerByName[key] = root.configured
				if enabled {
					activeCount++
				}
			}
			entries = append(entries, SkillDirEntry{
				Name:         s.Name,
				Description:  s.Description,
				Path:         s.Path,
				BaseDir:      s.BaseDir,
				Enabled:      enabled,
				OverriddenBy: overriddenBy,
			})
		}
		reports = append(reports, SkillDirReport{
			Scope:        root.scope,
			Configured:   root.configured,
			ResolvedPath: root.resolvedPath,
			Exists:       exists,
			Skills:       entries,
		})
	}

	return SkillInspection{
		Directories: reports,
		ActiveCount: activeCount,
		TotalCount:  totalCount,
	}
}

func loadSkillByNameWithConfig(name string, workdir string, cfg *SkillConfig) (SkillDocument, error) {
	trimmed := strings.TrimSpace(name)
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return SkillDocument{}, fmt.Errorf("skill name is required")
	}
	_, _, disabled := effectiveSkillConfig(cfg)
	if disabled[strings.ToLower(trimmed)] {
		return SkillDocument{}, fmt.Errorf("skill %q is disabled in settings", trimmed)
	}
	catalog := discoverSkillsWithConfig(workdir, cfg)
	for _, s := range catalog.Skills {
		if strings.EqualFold(s.Name, trimmed) {
			data, err := readFileBounded(s.Path, maxSkillFileBytes)
			if err != nil {
				return SkillDocument{}, fmt.Errorf("failed to read skill %q: %w", s.Name, err)
			}
			_, body := parseSkillFrontmatter(string(data))
			return SkillDocument{
				Name:        s.Name,
				Description: s.Description,
				Scope:       s.Scope,
				Path:        s.Path,
				BaseDir:     s.BaseDir,
				Content:     strings.TrimSpace(body),
			}, nil
		}
	}
	return SkillDocument{}, fmt.Errorf("skill %q not found", trimmed)
}

func readFileBounded(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	limit := maxBytes
	if info.Size() < limit {
		limit = info.Size()
	}
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

type skillFrontmatter struct {
	name        string
	description string
}

// parseSkillFrontmatter parses lightweight YAML frontmatter delimited by "---" lines.
func parseSkillFrontmatter(raw string) (skillFrontmatter, string) {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") && normalized != "---" {
		return skillFrontmatter{}, normalized
	}
	rest := strings.TrimPrefix(normalized, "---\n")
	lines := strings.Split(rest, "\n")

	endIdx := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			endIdx = i
			break
		}
	}
	if endIdx < 0 {
		return skillFrontmatter{}, normalized
	}

	fmLines := lines[:endIdx]
	body := strings.Join(lines[endIdx+1:], "\n")

	var fm skillFrontmatter
	for i := 0; i < len(fmLines); i++ {
		line := fmLines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		val := strings.TrimSpace(line[colon+1:])

		// Handle YAML block scalar (| or >)
		if val == "|" || val == ">" || val == "|-" || val == ">-" {
			var blockLines []string
			for i+1 < len(fmLines) {
				next := fmLines[i+1]
				if len(next) > 0 && (next[0] == ' ' || next[0] == '\t') {
					blockLines = append(blockLines, strings.TrimSpace(next))
					i++
				} else if strings.TrimSpace(next) == "" {
					blockLines = append(blockLines, "")
					i++
				} else {
					break
				}
			}
			sep := " "
			if strings.HasPrefix(val, "|") {
				sep = "\n"
			}
			val = strings.TrimSpace(strings.Join(blockLines, sep))
		} else {
			val = unquoteYAMLValue(val)
		}

		switch key {
		case "name":
			fm.name = val
		case "description":
			fm.description = val
		}
	}
	return fm, body
}

func unquoteYAMLValue(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func executeSkill(req ToolExecuteRequest) (ToolExecuteResult, error) {
	name := strings.TrimSpace(stringInput(req.Input, "name"))
	if name == "" {
		return toolErrorResult("Error: name is required", req.Workdir)
	}
	doc, err := loadSkillByNameWithConfig(name, req.Workdir, req.SkillConfig)
	if err != nil {
		return toolErrorResult("Error: "+err.Error(), req.Workdir)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "# Skill: %s (%s)\n", doc.Name, doc.Scope)
	if doc.Description != "" {
		fmt.Fprintf(&out, "Description: %s\n", doc.Description)
	}
	fmt.Fprintf(&out, "Base directory: %s\n\n", doc.BaseDir)
	out.WriteString(doc.Content)

	return ToolExecuteResult{
		ModelContent: out.String(),
		IsError:      false,
		UIMeta: map[string]any{
			"skillName":  doc.Name,
			"skillScope": doc.Scope,
			"skillPath":  doc.Path,
		},
	}, nil
}
