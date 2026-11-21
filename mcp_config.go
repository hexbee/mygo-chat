package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const mcpConfigFileVersion = 1

type mcpConfigFile struct {
	Version int                        `json:"version"`
	Servers map[string]MCPServerConfig `json:"servers"`
}

func defaultMCPConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("cannot determine user home directory: %w", err)
	}
	return filepath.Join(home, ".vela", "mcp.json"), nil
}

func loadMCPConfigsFromFile(path string) (map[string]MCPServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]MCPServerConfig), nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return make(map[string]MCPServerConfig), nil
	}
	var file mcpConfigFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed to parse MCP config %s: %w", path, err)
	}
	result := make(map[string]MCPServerConfig, len(file.Servers))
	for name, cfg := range file.Servers {
		if strings.TrimSpace(cfg.Name) == "" {
			cfg.Name = name
		}
		cfg = normalizeMCPServerConfig(cfg)
		if len(validateMCPServerConfig(cfg)) == 0 {
			result[cfg.Name] = cfg
		}
	}
	return result, nil
}

// saveMCPConfigsToFile writes ~/.vela/mcp.json atomically with 0600 permissions
// so static HTTP Authorization headers / tokens are never world-readable.
func saveMCPConfigsToFile(path string, servers map[string]MCPServerConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	file := mcpConfigFile{
		Version: mcpConfigFileVersion,
		Servers: make(map[string]MCPServerConfig, len(servers)),
	}
	for name, cfg := range servers {
		file.Servers[name] = normalizeMCPServerConfig(cfg)
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".mcp-*.json.tmp")
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

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	_ = os.Chmod(path, 0o600)
	return nil
}

func sortedMCPConfigs(servers map[string]MCPServerConfig) []MCPServerConfig {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]MCPServerConfig, 0, len(names))
	for _, name := range names {
		out = append(out, cloneMCPServerConfig(servers[name]))
	}
	return out
}
