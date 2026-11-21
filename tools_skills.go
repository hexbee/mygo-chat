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

// SkillSummary describes a discovered skill for progressive catalog disclosure.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"` // "project" | "user"
	Path        string `json:"path"`
	BaseDir     string `json:"baseDir"`
}

// SkillCatalog contains all discovered skills and a stable content digest.
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

// SkillService discovers and loads local SKILL.md files from <workspace>/.agents/skills and ~/.vela/skills.
type SkillService struct{}

// ListSkills scans project and user skill directories (one level deep) and returns metadata plus a digest.
func (SkillService) ListSkills(workdir string) (SkillCatalog, error) {
	return discoverSkills(workdir), nil
}

// ReadSkill loads the full markdown body of a skill by name.
func (SkillService) ReadSkill(name string, workdir string) (SkillDocument, error) {
	return loadSkillByName(name, workdir)
}

func skillRoots(workdir string) []struct {
	dir   string
	scope string
} {
	var roots []struct {
		dir   string
		scope string
	}
	if trimmed := strings.TrimSpace(workdir); trimmed != "" {
		clean := filepath.Clean(trimmed)
		roots = append(roots,
			struct {
				dir   string
				scope string
			}{dir: filepath.Join(clean, ".agents", "skills"), scope: skillScopeProject},
			struct {
				dir   string
				scope string
			}{dir: filepath.Join(clean, ".agent", "skills"), scope: skillScopeProject},
		)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		roots = append(roots,
			struct {
				dir   string
				scope string
			}{dir: filepath.Join(home, ".vela", "skills"), scope: skillScopeUser},
			struct {
				dir   string
				scope string
			}{dir: filepath.Join(home, ".agents", "skills"), scope: skillScopeUser},
			struct {
				dir   string
				scope string
			}{dir: filepath.Join(home, ".agent", "skills"), scope: skillScopeUser},
		)
	}
	return roots
}

func discoverSkills(workdir string) SkillCatalog {
	byName := make(map[string]SkillSummary)
	for _, root := range skillRoots(workdir) {
		entries, err := os.ReadDir(root.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			entryName := entry.Name()
			if strings.HasPrefix(entryName, ".") {
				continue
			}
			fullPath := filepath.Join(root.dir, entryName)
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
				baseDir = root.dir
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
			key := strings.ToLower(name)
			// Project skills override user skills with the same name.
			if _, exists := byName[key]; exists {
				continue
			}
			byName[key] = SkillSummary{
				Name:        name,
				Description: strings.TrimSpace(fm.description),
				Scope:       root.scope,
				Path:        skillFile,
				BaseDir:     baseDir,
			}
		}
	}

	skills := make([]SkillSummary, 0, len(byName))
	for _, s := range byName {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Scope != skills[j].Scope {
			return skills[i].Scope == skillScopeProject
		}
		return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
	})

	h := sha256.New()
	for _, s := range skills {
		_, _ = fmt.Fprintf(h, "%s|%s|%s|%s\n", s.Scope, s.Name, s.Description, s.Path)
	}
	digest := hex.EncodeToString(h.Sum(nil))[:16]
	if len(skills) == 0 {
		digest = ""
	}

	return SkillCatalog{
		Skills: skills,
		Digest: digest,
	}
}

func loadSkillByName(name string, workdir string) (SkillDocument, error) {
	trimmed := strings.TrimSpace(name)
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return SkillDocument{}, fmt.Errorf("skill name is required")
	}
	catalog := discoverSkills(workdir)
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
	doc, err := loadSkillByName(name, req.Workdir)
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
