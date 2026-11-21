package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const (
	MCPTransportHTTP  = "streamable-http"
	MCPTransportStdio = "stdio"

	MCPToolLoadingOnDemand = "on-demand"
	MCPToolLoadingEager    = "eager"

	MCPStateDisabled       = "disabled"
	MCPStateConnecting     = "connecting"
	MCPStateConnected      = "connected"
	MCPStateReconnecting   = "reconnecting"
	MCPStateError          = "error"
	MCPStateMissingRuntime = "missing-runtime"

	mcpLoaderRawName            = "load_tools"
	mcpMaxPublicNameLen         = 64
	mcpHashLen                  = 12
	mcpDefaultToolCallTimeoutMs = 60_000
	mcpMinToolCallTimeoutMs     = 1_000
	mcpMaxToolCallTimeoutMs     = 300_000
	mcpDefaultReconnectInitMs   = 500
	mcpDefaultReconnectMaxMs    = 30_000
	mcpDefaultReconnectAttempts = 10
	mcpMaxInstructionBytes      = 32 * 1024
	mcpProbeTimeoutMs           = 10_000
	mcpProbeMaxPages            = 16
	mcpProbeMaxTools            = 512
	mcpRedactedPlaceholder      = "[redacted]"
)

var (
	mcpServerNamePattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	mcpInvalidToolNameChar = regexp.MustCompile(`[^A-Za-z0-9_-]`)
)

// MCPReconnectConfig controls bounded exponential backoff reconnection for a server.
type MCPReconnectConfig struct {
	Enabled        bool `json:"enabled"`
	InitialDelayMs int  `json:"initialDelayMs,omitempty"`
	MaxDelayMs     int  `json:"maxDelayMs,omitempty"`
	MaxAttempts    int  `json:"maxAttempts,omitempty"`
}

// MCPServerConfig defines one configured MCP server persisted in ~/.vela/mcp.json.
type MCPServerConfig struct {
	Name              string              `json:"name"`
	Transport         string              `json:"transport"` // "streamable-http" | "stdio"
	Enabled           bool                `json:"enabled"`
	ToolLoading       string              `json:"toolLoading,omitempty"` // "on-demand" (default) | "eager"
	TrustServer       bool                `json:"trustServer,omitempty"`
	ToolCallTimeoutMs int                 `json:"toolCallTimeoutMs,omitempty"`
	Reconnect         *MCPReconnectConfig `json:"reconnect,omitempty"`

	// Streamable HTTP fields
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// Stdio fields
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
}

// MCPServerInfo carries the server identity returned by MCP initialize.
type MCPServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// MCPToolInfo describes a single tool bridged from an MCP server.
type MCPToolInfo struct {
	PublicName   string         `json:"publicName"`
	RawName      string         `json:"rawName"`
	ServerName   string         `json:"serverName"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	ReadOnlyHint bool           `json:"readOnlyHint"`
}

// MCPRuntimeCheck reports the preflight lookup result for a stdio command.
type MCPRuntimeCheck struct {
	Found        bool   `json:"found"`
	Command      string `json:"command"`
	ResolvedPath string `json:"resolvedPath,omitempty"`
	Kind         string `json:"kind"` // "npx" | "uvx" | "binary"
	Message      string `json:"message,omitempty"`
}

// MCPServerStatus is the live runtime status of a single MCP server.
type MCPServerStatus struct {
	Name          string           `json:"name"`
	Transport     string           `json:"transport"`
	Enabled       bool             `json:"enabled"`
	State         string           `json:"state"` // disabled | connecting | connected | reconnecting | error | missing-runtime
	ToolCount     int              `json:"toolCount"`
	ServerInfo    *MCPServerInfo   `json:"serverInfo,omitempty"`
	Instructions  string           `json:"instructions,omitempty"`
	Tools         []MCPToolInfo    `json:"tools,omitempty"`
	LastError     string           `json:"lastError,omitempty"`
	RuntimeHint   *MCPRuntimeCheck `json:"runtimeHint,omitempty"`
	Attempt       int              `json:"attempt,omitempty"`
	MaxAttempts   int              `json:"maxAttempts,omitempty"`
	NextRetryAtMs int64            `json:"nextRetryAtMs,omitempty"`
	ConnectedAtMs int64            `json:"connectedAtMs,omitempty"`
}

// MCPSnapshot is pushed to the frontend via MCPStateEvent and returned by GetSnapshot.
type MCPSnapshot struct {
	Servers  []MCPServerConfig          `json:"servers"`
	Statuses map[string]MCPServerStatus `json:"statuses"`
}

// MCPProbeResult is the stage-classified outcome of a one-shot side-effect-free probe.
type MCPProbeResult struct {
	OK           bool             `json:"ok"`
	Stage        string           `json:"stage"` // validate | runtime | connect | list
	ServerInfo   *MCPServerInfo   `json:"serverInfo,omitempty"`
	ToolCount    int              `json:"toolCount"`
	Tools        []MCPToolInfo    `json:"tools,omitempty"`
	Truncated    bool             `json:"truncated,omitempty"`
	Instructions string           `json:"instructions,omitempty"`
	DurationMs   int64            `json:"durationMs"`
	Message      string           `json:"message,omitempty"`
	RuntimeHint  *MCPRuntimeCheck `json:"runtimeHint,omitempty"`
}

// MCPValidationError describes one field-level configuration validation issue.
type MCPValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// MCPImportResult returns validation diagnostics and parsed servers for JSON import.
type MCPImportResult struct {
	Valid   bool                 `json:"valid"`
	Applied bool                 `json:"applied"`
	Count   int                  `json:"count"`
	Names   []string             `json:"names,omitempty"`
	Errors  []MCPValidationError `json:"errors,omitempty"`
}

// mcpPublicToolName derives the deterministic model-facing tool name for (serverName, rawName),
// matching deepseek-harness's naming contract (max 64 chars, [A-Za-z0-9_-], 12-hex SHA-256 on lossy normalization).
func mcpPublicToolName(serverName, rawName string) string {
	joined := "mcp__" + serverName + "__" + rawName
	normalized := mcpInvalidToolNameChar.ReplaceAllString(joined, "_")
	if normalized == joined && len(normalized) <= mcpMaxPublicNameLen {
		return normalized
	}
	sum := sha256.Sum256([]byte(serverName + "\x00" + rawName))
	hash := hex.EncodeToString(sum[:])[:mcpHashLen]
	prefixLen := mcpMaxPublicNameLen - mcpHashLen - 1
	if len(normalized) > prefixLen {
		normalized = normalized[:prefixLen]
	}
	return normalized + "_" + hash
}

func mcpLoaderPublicName(serverName string) string {
	return mcpPublicToolName(serverName, mcpLoaderRawName)
}

func normalizeMCPServerConfig(cfg MCPServerConfig) MCPServerConfig {
	out := cfg
	out.Name = strings.TrimSpace(out.Name)
	out.Transport = strings.ToLower(strings.TrimSpace(out.Transport))
	if out.Transport == "" {
		if strings.TrimSpace(out.Command) != "" && strings.TrimSpace(out.URL) == "" {
			out.Transport = MCPTransportStdio
		} else {
			out.Transport = MCPTransportHTTP
		}
	}
	out.ToolLoading = strings.ToLower(strings.TrimSpace(out.ToolLoading))
	if out.ToolLoading != MCPToolLoadingEager {
		out.ToolLoading = MCPToolLoadingOnDemand
	}
	if out.ToolCallTimeoutMs <= 0 {
		out.ToolCallTimeoutMs = mcpDefaultToolCallTimeoutMs
	} else if out.ToolCallTimeoutMs < mcpMinToolCallTimeoutMs {
		out.ToolCallTimeoutMs = mcpMinToolCallTimeoutMs
	} else if out.ToolCallTimeoutMs > mcpMaxToolCallTimeoutMs {
		out.ToolCallTimeoutMs = mcpMaxToolCallTimeoutMs
	}
	if out.Reconnect == nil {
		out.Reconnect = &MCPReconnectConfig{
			Enabled:        true,
			InitialDelayMs: mcpDefaultReconnectInitMs,
			MaxDelayMs:     mcpDefaultReconnectMaxMs,
			MaxAttempts:    mcpDefaultReconnectAttempts,
		}
	} else {
		rc := *out.Reconnect
		if rc.InitialDelayMs <= 0 {
			rc.InitialDelayMs = mcpDefaultReconnectInitMs
		}
		if rc.MaxDelayMs <= 0 {
			rc.MaxDelayMs = mcpDefaultReconnectMaxMs
		}
		if rc.InitialDelayMs > rc.MaxDelayMs {
			rc.MaxDelayMs = rc.InitialDelayMs
		}
		if rc.MaxAttempts <= 0 {
			rc.MaxAttempts = mcpDefaultReconnectAttempts
		}
		out.Reconnect = &rc
	}
	out.URL = strings.TrimSpace(out.URL)
	out.Command = strings.TrimSpace(out.Command)
	out.Cwd = strings.TrimSpace(out.Cwd)

	if len(out.Headers) > 0 {
		clean := make(map[string]string, len(out.Headers))
		for k, v := range out.Headers {
			tk := strings.TrimSpace(k)
			if tk != "" {
				clean[tk] = v
			}
		}
		out.Headers = clean
	} else {
		out.Headers = nil
	}

	if len(out.Env) > 0 {
		clean := make(map[string]string, len(out.Env))
		for k, v := range out.Env {
			tk := strings.TrimSpace(k)
			if tk != "" {
				clean[tk] = v
			}
		}
		out.Env = clean
	} else {
		out.Env = nil
	}

	if len(out.Args) > 0 {
		argsCopy := make([]string, len(out.Args))
		copy(argsCopy, out.Args)
		out.Args = argsCopy
	} else {
		out.Args = nil
	}

	return out
}

func validateMCPServerConfig(cfg MCPServerConfig) []MCPValidationError {
	var errs []MCPValidationError
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		errs = append(errs, MCPValidationError{Field: "name", Message: "server name is required"})
	} else if !mcpServerNamePattern.MatchString(name) {
		errs = append(errs, MCPValidationError{Field: "name", Message: "server name must match [A-Za-z0-9_-]{1,32}"})
	}

	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))
	if transport != MCPTransportHTTP && transport != MCPTransportStdio {
		errs = append(errs, MCPValidationError{Field: "transport", Message: "transport must be streamable-http or stdio"})
	}

	if cfg.ToolLoading != "" && cfg.ToolLoading != MCPToolLoadingOnDemand && cfg.ToolLoading != MCPToolLoadingEager {
		errs = append(errs, MCPValidationError{Field: "toolLoading", Message: "toolLoading must be on-demand or eager"})
	}

	if cfg.ToolCallTimeoutMs != 0 && (cfg.ToolCallTimeoutMs < mcpMinToolCallTimeoutMs || cfg.ToolCallTimeoutMs > mcpMaxToolCallTimeoutMs) {
		errs = append(errs, MCPValidationError{
			Field:   "toolCallTimeoutMs",
			Message: fmt.Sprintf("toolCallTimeoutMs must be between %d and %d", mcpMinToolCallTimeoutMs, mcpMaxToolCallTimeoutMs),
		})
	}

	switch transport {
	case MCPTransportHTTP:
		rawURL := strings.TrimSpace(cfg.URL)
		if rawURL == "" {
			errs = append(errs, MCPValidationError{Field: "url", Message: "URL is required for streamable-http transport"})
		} else {
			u, err := url.Parse(rawURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				errs = append(errs, MCPValidationError{Field: "url", Message: "URL must be a valid http:// or https:// endpoint"})
			}
		}
	case MCPTransportStdio:
		if strings.TrimSpace(cfg.Command) == "" {
			errs = append(errs, MCPValidationError{Field: "command", Message: "command is required for stdio transport"})
		}
	}

	return errs
}

func cloneMCPServerConfig(cfg MCPServerConfig) MCPServerConfig {
	out := cfg
	if cfg.Reconnect != nil {
		rc := *cfg.Reconnect
		out.Reconnect = &rc
	}
	if cfg.Headers != nil {
		out.Headers = make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			out.Headers[k] = v
		}
	}
	if cfg.Args != nil {
		out.Args = make([]string, len(cfg.Args))
		copy(out.Args, cfg.Args)
	}
	if cfg.Env != nil {
		out.Env = make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			out.Env[k] = v
		}
	}
	return out
}

func cloneMCPServerStatus(st MCPServerStatus) MCPServerStatus {
	out := st
	if st.ServerInfo != nil {
		info := *st.ServerInfo
		out.ServerInfo = &info
	}
	if st.RuntimeHint != nil {
		hint := *st.RuntimeHint
		out.RuntimeHint = &hint
	}
	if st.Tools != nil {
		out.Tools = make([]MCPToolInfo, len(st.Tools))
		copy(out.Tools, st.Tools)
	}
	return out
}

// collectMCPSecrets gathers secret values from headers and sensitive env vars for redaction.
func collectMCPSecrets(cfg MCPServerConfig) []string {
	seen := make(map[string]struct{})
	add := func(val string) {
		trimmed := strings.TrimSpace(val)
		if len(trimmed) < 4 {
			return
		}
		seen[trimmed] = struct{}{}
		// Also redact the raw token after "Bearer " or "Basic " prefix.
		for _, prefix := range []string{"Bearer ", "bearer ", "Basic ", "basic ", "Token ", "token "} {
			if strings.HasPrefix(trimmed, prefix) {
				tok := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
				if len(tok) >= 4 {
					seen[tok] = struct{}{}
				}
			}
		}
	}
	for _, v := range cfg.Headers {
		add(v)
	}
	if cfg.URL != "" {
		if u, err := url.Parse(cfg.URL); err == nil {
			if u.User != nil {
				if pw, ok := u.User.Password(); ok {
					add(pw)
				}
			}
			for k, vals := range u.Query() {
				if sensitiveEnvPattern.MatchString(k) || strings.EqualFold(k, "auth") {
					for _, v := range vals {
						add(v)
					}
				}
			}
		}
	}
	for k, v := range cfg.Env {
		if sensitiveEnvPattern.MatchString(k) {
			add(v)
		}
	}
	secrets := make([]string, 0, len(seen))
	for s := range seen {
		secrets = append(secrets, s)
	}
	// Longest first so a secret containing a shorter secret is redacted whole.
	sort.Slice(secrets, func(i, j int) bool {
		if len(secrets[i]) != len(secrets[j]) {
			return len(secrets[i]) > len(secrets[j])
		}
		return secrets[i] < secrets[j]
	})
	return secrets
}

// redactMCPSecrets replaces known configured secrets with "[redacted]".
func redactMCPSecrets(text string, cfg MCPServerConfig) string {
	if text == "" {
		return ""
	}
	secrets := collectMCPSecrets(cfg)
	out := text
	for _, secret := range secrets {
		out = strings.ReplaceAll(out, secret, mcpRedactedPlaceholder)
	}
	return out
}

// parseMCPServersImportJSON parses standard `{ "mcpServers": { ... } }` or a bare `{ "<name>": { ... } }` map.
func parseMCPServersImportJSON(rawJSON string) ([]MCPServerConfig, []MCPValidationError) {
	trimmed := strings.TrimSpace(rawJSON)
	if trimmed == "" {
		return nil, []MCPValidationError{{Field: "json", Message: "JSON content is empty"}}
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &root); err != nil {
		return nil, []MCPValidationError{{Field: "json", Message: fmt.Sprintf("invalid JSON: %v", err)}}
	}
	if len(root) == 0 {
		return nil, []MCPValidationError{{Field: "json", Message: "JSON object is empty"}}
	}

	entriesMap := root
	if rawServers, ok := root["mcpServers"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(rawServers, &nested); err != nil {
			return nil, []MCPValidationError{{Field: "mcpServers", Message: fmt.Sprintf("mcpServers must be an object: %v", err)}}
		}
		entriesMap = nested
	} else if rawServers, ok := root["servers"]; ok {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(rawServers, &nested); err != nil {
			return nil, []MCPValidationError{{Field: "servers", Message: fmt.Sprintf("servers must be an object: %v", err)}}
		}
		entriesMap = nested
	}

	if len(entriesMap) == 0 {
		return nil, []MCPValidationError{{Field: "mcpServers", Message: "no MCP servers defined"}}
	}

	names := make([]string, 0, len(entriesMap))
	for name := range entriesMap {
		names = append(names, name)
	}
	sort.Strings(names)

	type rawServerEntry struct {
		Transport         string              `json:"transport"`
		Type              string              `json:"type"`
		URL               string              `json:"url"`
		HTTPUrl           string              `json:"httpUrl"`
		ServerURL         string              `json:"serverUrl"`
		Headers           map[string]string   `json:"headers"`
		Command           string              `json:"command"`
		Args              []string            `json:"args"`
		Env               map[string]string   `json:"env"`
		Cwd               string              `json:"cwd"`
		Enabled           *bool               `json:"enabled"`
		Disabled          *bool               `json:"disabled"`
		ToolLoading       string              `json:"toolLoading"`
		TrustServer       bool                `json:"trustServer"`
		ToolCallTimeoutMs int                 `json:"toolCallTimeoutMs"`
		Reconnect         *MCPReconnectConfig `json:"reconnect"`
	}

	var parsed []MCPServerConfig
	var allErrs []MCPValidationError
	for _, name := range names {
		var entry rawServerEntry
		if err := json.Unmarshal(entriesMap[name], &entry); err != nil {
			allErrs = append(allErrs, MCPValidationError{
				Field:   "mcpServers." + name,
				Message: fmt.Sprintf("invalid server object: %v", err),
			})
			continue
		}
		endpointURL := strings.TrimSpace(entry.URL)
		if endpointURL == "" {
			endpointURL = strings.TrimSpace(entry.HTTPUrl)
		}
		if endpointURL == "" {
			endpointURL = strings.TrimSpace(entry.ServerURL)
		}
		transport := strings.ToLower(strings.TrimSpace(entry.Transport))
		if transport == "" {
			transport = strings.ToLower(strings.TrimSpace(entry.Type))
		}
		switch transport {
		case "http", "streamable_http", "streamablehttp":
			transport = MCPTransportHTTP
		case "":
			if strings.TrimSpace(entry.Command) != "" && endpointURL == "" {
				transport = MCPTransportStdio
			} else {
				transport = MCPTransportHTTP
			}
		}

		enabled := true
		if entry.Enabled != nil {
			enabled = *entry.Enabled
		} else if entry.Disabled != nil {
			enabled = !*entry.Disabled
		}

		cfg := MCPServerConfig{
			Name:              name,
			Transport:         transport,
			Enabled:           enabled,
			ToolLoading:       entry.ToolLoading,
			TrustServer:       entry.TrustServer,
			ToolCallTimeoutMs: entry.ToolCallTimeoutMs,
			Reconnect:         entry.Reconnect,
			URL:               endpointURL,
			Headers:           entry.Headers,
			Command:           entry.Command,
			Args:              entry.Args,
			Env:               entry.Env,
			Cwd:               entry.Cwd,
		}
		verrs := validateMCPServerConfig(cfg)
		if len(verrs) > 0 {
			for _, ve := range verrs {
				allErrs = append(allErrs, MCPValidationError{
					Field:   fmt.Sprintf("mcpServers.%s.%s", name, ve.Field),
					Message: ve.Message,
				})
			}
			continue
		}
		parsed = append(parsed, normalizeMCPServerConfig(cfg))
	}

	return parsed, allErrs
}
