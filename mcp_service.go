package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// MCPStateEvent pushes live MCP server configuration and connection status updates to the frontend.
var MCPStateEvent = mygo.NewEvent[MCPSnapshot]("mcp:state")

type mcpServerRunner struct {
	cancel context.CancelFunc
	done   chan struct{}
	client mcpWireClient
}

// MCPService manages MCP server configurations (~/.vela/mcp.json), supervised connections,
// tool snapshots, one-shot probes, and tool execution dispatch.
type MCPService struct {
	mu            sync.RWMutex
	configPath    string
	servers       map[string]MCPServerConfig
	statuses      map[string]MCPServerStatus
	runners       map[string]*mcpServerRunner
	toolIndex     map[string]mcpIndexedTool // publicName -> indexed tool
	started       bool
	emitFn        func(MCPSnapshot)
	httpTransport http.RoundTripper
}

type mcpIndexedTool struct {
	serverName string
	rawName    string
	tool       MCPToolInfo
}

var globalMCPService = newMCPService("")

func newMCPService(configPath string) *MCPService {
	return &MCPService{
		configPath: configPath,
		servers:    make(map[string]MCPServerConfig),
		statuses:   make(map[string]MCPServerStatus),
		runners:    make(map[string]*mcpServerRunner),
		toolIndex:  make(map[string]mcpIndexedTool),
	}
}

func (s *MCPService) resolvedConfigPath() (string, error) {
	s.mu.RLock()
	p := s.configPath
	s.mu.RUnlock()
	if p != "" {
		return p, nil
	}
	return defaultMCPConfigPath()
}

// Start loads ~/.vela/mcp.json and asynchronously connects all enabled servers.
func (s *MCPService) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.mu.Unlock()

	path, err := s.resolvedConfigPath()
	if err != nil {
		log.Printf("mcp: failed to resolve config path: %v", err)
		return
	}
	loaded, err := loadMCPConfigsFromFile(path)
	if err != nil {
		log.Printf("mcp: failed to load config: %v", err)
		return
	}

	s.mu.Lock()
	for name, cfg := range loaded {
		s.servers[name] = cfg
		state := MCPStateDisabled
		if cfg.Enabled {
			state = MCPStateConnecting
		}
		s.statuses[name] = MCPServerStatus{
			Name:      cfg.Name,
			Transport: cfg.Transport,
			Enabled:   cfg.Enabled,
			State:     state,
		}
	}
	toStart := make([]MCPServerConfig, 0, len(loaded))
	for _, cfg := range loaded {
		if cfg.Enabled {
			toStart = append(toStart, cloneMCPServerConfig(cfg))
		}
	}
	s.mu.Unlock()

	for _, cfg := range toStart {
		s.startServerRunner(cfg)
	}
	s.emitSnapshot()
}

// StopAll shuts down all active MCP server connections cleanly.
func (s *MCPService) StopAll() {
	s.mu.Lock()
	names := make([]string, 0, len(s.runners))
	for name := range s.runners {
		names = append(names, name)
	}
	s.mu.Unlock()
	for _, name := range names {
		s.stopServerRunner(name)
	}
}

func (s *MCPService) emitSnapshot() {
	snap := s.GetSnapshot()
	s.mu.RLock()
	emitFn := s.emitFn
	s.mu.RUnlock()
	if emitFn != nil {
		emitFn(snap)
		return
	}
	if win := getMainWindow(); win != nil {
		_ = MCPStateEvent.Emit(win, snap)
	}
}

// GetSnapshot returns all configured MCP servers and their live runtime statuses.
func (s *MCPService) GetSnapshot() MCPSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statuses := make(map[string]MCPServerStatus, len(s.statuses))
	for name, st := range s.statuses {
		statuses[name] = cloneMCPServerStatus(st)
	}
	return MCPSnapshot{
		Servers:  sortedMCPConfigs(s.servers),
		Statuses: statuses,
	}
}

// CheckRuntime runs a real-time LookPath preflight for a stdio command using the login shell PATH.
func (s *MCPService) CheckRuntime(command string) MCPRuntimeCheck {
	return checkMCPRuntime(command)
}

// UpsertServer validates, persists, and starts or stops an MCP server configuration.
func (s *MCPService) UpsertServer(cfg MCPServerConfig) (MCPSnapshot, error) {
	cfg = normalizeMCPServerConfig(cfg)
	if verrs := validateMCPServerConfig(cfg); len(verrs) > 0 {
		return s.GetSnapshot(), fmt.Errorf("%s: %s", verrs[0].Field, verrs[0].Message)
	}

	s.stopServerRunner(cfg.Name)

	s.mu.Lock()
	s.servers[cfg.Name] = cloneMCPServerConfig(cfg)
	state := MCPStateDisabled
	if cfg.Enabled {
		state = MCPStateConnecting
	}
	s.statuses[cfg.Name] = MCPServerStatus{
		Name:      cfg.Name,
		Transport: cfg.Transport,
		Enabled:   cfg.Enabled,
		State:     state,
	}
	s.rebuildToolIndexLocked()
	serversCopy := make(map[string]MCPServerConfig, len(s.servers))
	for k, v := range s.servers {
		serversCopy[k] = cloneMCPServerConfig(v)
	}
	s.mu.Unlock()

	if err := s.persistServers(serversCopy); err != nil {
		return s.GetSnapshot(), err
	}

	if cfg.Enabled {
		s.startServerRunner(cfg)
	}
	s.emitSnapshot()
	return s.GetSnapshot(), nil
}

// DeleteServer stops and removes a configured MCP server from ~/.vela/mcp.json.
func (s *MCPService) DeleteServer(name string) (MCPSnapshot, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return s.GetSnapshot(), fmt.Errorf("server name is required")
	}

	s.stopServerRunner(name)

	s.mu.Lock()
	delete(s.servers, name)
	delete(s.statuses, name)
	s.rebuildToolIndexLocked()
	serversCopy := make(map[string]MCPServerConfig, len(s.servers))
	for k, v := range s.servers {
		serversCopy[k] = cloneMCPServerConfig(v)
	}
	s.mu.Unlock()

	if err := s.persistServers(serversCopy); err != nil {
		return s.GetSnapshot(), err
	}
	s.emitSnapshot()
	return s.GetSnapshot(), nil
}

// SetServerEnabled enables or disables an existing MCP server.
func (s *MCPService) SetServerEnabled(name string, enabled bool) (MCPSnapshot, error) {
	name = strings.TrimSpace(name)
	s.mu.RLock()
	cfg, ok := s.servers[name]
	s.mu.RUnlock()
	if !ok {
		return s.GetSnapshot(), fmt.Errorf("MCP server %q not found", name)
	}
	cfg.Enabled = enabled
	return s.UpsertServer(cfg)
}

// RestartServer resets the reconnect budget and reconnects an enabled MCP server.
func (s *MCPService) RestartServer(name string) (MCPSnapshot, error) {
	name = strings.TrimSpace(name)
	s.mu.RLock()
	cfg, ok := s.servers[name]
	s.mu.RUnlock()
	if !ok {
		return s.GetSnapshot(), fmt.Errorf("MCP server %q not found", name)
	}
	if !cfg.Enabled {
		return s.GetSnapshot(), nil
	}
	s.stopServerRunner(name)
	s.mu.Lock()
	s.statuses[name] = MCPServerStatus{
		Name:      cfg.Name,
		Transport: cfg.Transport,
		Enabled:   true,
		State:     MCPStateConnecting,
	}
	s.rebuildToolIndexLocked()
	s.mu.Unlock()
	s.startServerRunner(cfg)
	s.emitSnapshot()
	return s.GetSnapshot(), nil
}

// ImportConfigJSON validates a `{ "mcpServers": { ... } }` snippet and optionally applies it.
func (s *MCPService) ImportConfigJSON(rawJSON string, apply bool) (MCPImportResult, error) {
	parsed, verrs := parseMCPServersImportJSON(rawJSON)
	names := make([]string, 0, len(parsed))
	for _, cfg := range parsed {
		names = append(names, cfg.Name)
	}
	if len(verrs) > 0 {
		return MCPImportResult{
			Valid:   false,
			Applied: false,
			Count:   len(parsed),
			Names:   names,
			Errors:  verrs,
		}, nil
	}
	if !apply {
		return MCPImportResult{
			Valid:   true,
			Applied: false,
			Count:   len(parsed),
			Names:   names,
		}, nil
	}

	for _, cfg := range parsed {
		if _, err := s.UpsertServer(cfg); err != nil {
			return MCPImportResult{
				Valid:   false,
				Applied: false,
				Count:   len(parsed),
				Names:   names,
				Errors:  []MCPValidationError{{Field: "mcpServers." + cfg.Name, Message: err.Error()}},
			}, nil
		}
	}
	return MCPImportResult{
		Valid:   true,
		Applied: true,
		Count:   len(parsed),
		Names:   names,
	}, nil
}

// ExportConfigJSON returns the current server configs formatted as `{ "mcpServers": { ... } }`.
func (s *MCPService) ExportConfigJSON() (string, error) {
	s.mu.RLock()
	servers := sortedMCPConfigs(s.servers)
	s.mu.RUnlock()

	type exportEntry struct {
		Transport         string              `json:"transport"`
		URL               string              `json:"url,omitempty"`
		Headers           map[string]string   `json:"headers,omitempty"`
		Command           string              `json:"command,omitempty"`
		Args              []string            `json:"args,omitempty"`
		Env               map[string]string   `json:"env,omitempty"`
		Cwd               string              `json:"cwd,omitempty"`
		Enabled           bool                `json:"enabled"`
		ToolLoading       string              `json:"toolLoading,omitempty"`
		TrustServer       bool                `json:"trustServer,omitempty"`
		ToolCallTimeoutMs int                 `json:"toolCallTimeoutMs,omitempty"`
		Reconnect         *MCPReconnectConfig `json:"reconnect,omitempty"`
	}
	outMap := make(map[string]exportEntry, len(servers))
	for _, cfg := range servers {
		outMap[cfg.Name] = exportEntry{
			Transport:         cfg.Transport,
			URL:               cfg.URL,
			Headers:           cfg.Headers,
			Command:           cfg.Command,
			Args:              cfg.Args,
			Env:               cfg.Env,
			Cwd:               cfg.Cwd,
			Enabled:           cfg.Enabled,
			ToolLoading:       cfg.ToolLoading,
			TrustServer:       cfg.TrustServer,
			ToolCallTimeoutMs: cfg.ToolCallTimeoutMs,
			Reconnect:         cfg.Reconnect,
		}
	}
	wrapper := map[string]any{"mcpServers": outMap}
	b, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ProbeServer performs a one-shot, side-effect-free connection + tools/list check.
func (s *MCPService) ProbeServer(cfg MCPServerConfig) MCPProbeResult {
	started := time.Now()
	cfg = normalizeMCPServerConfig(cfg)
	if verrs := validateMCPServerConfig(cfg); len(verrs) > 0 {
		return MCPProbeResult{
			OK:         false,
			Stage:      "validate",
			DurationMs: time.Since(started).Milliseconds(),
			Message:    fmt.Sprintf("%s: %s", verrs[0].Field, verrs[0].Message),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), mcpProbeTimeoutMs*time.Millisecond)
	defer cancel()

	var wire mcpWireClient
	if cfg.Transport == MCPTransportStdio {
		resolvedPATH := resolveLoginShellPATH()
		rt := checkMCPRuntimeWithPATH(cfg.Command, resolvedPATH)
		if !rt.Found {
			return MCPProbeResult{
				OK:          false,
				Stage:       "runtime",
				DurationMs:  time.Since(started).Milliseconds(),
				Message:     rt.Message,
				RuntimeHint: &rt,
			}
		}
		var err error
		wire, err = startMCPStdioClient(cfg, rt.ResolvedPath, resolvedPATH, nil)
		if err != nil {
			return MCPProbeResult{
				OK:         false,
				Stage:      "connect",
				DurationMs: time.Since(started).Milliseconds(),
				Message:    redactMCPSecrets(err.Error(), cfg),
			}
		}
	} else {
		wire = newMCPHTTPClient(cfg, nil, s.httpTransport)
	}
	defer func() { _ = wire.Close() }()

	initRes, err := wire.Initialize(ctx)
	if err != nil {
		return MCPProbeResult{
			OK:         false,
			Stage:      "connect",
			DurationMs: time.Since(started).Milliseconds(),
			Message:    redactMCPSecrets(err.Error(), cfg),
		}
	}

	rawTools, truncated, err := wire.ListTools(ctx, mcpProbeMaxPages, mcpProbeMaxTools)
	if err != nil {
		return MCPProbeResult{
			OK:         false,
			Stage:      "list",
			ServerInfo: initRes.ServerInfo,
			DurationMs: time.Since(started).Milliseconds(),
			Message:    redactMCPSecrets(err.Error(), cfg),
		}
	}

	tools, mapErr := mapWireTools(cfg.Name, rawTools)
	if mapErr != nil {
		return MCPProbeResult{
			OK:         false,
			Stage:      "list",
			ServerInfo: initRes.ServerInfo,
			DurationMs: time.Since(started).Milliseconds(),
			Message:    redactMCPSecrets(mapErr.Error(), cfg),
		}
	}

	instructions := strings.TrimSpace(initRes.Instructions)
	if len(instructions) > mcpMaxInstructionBytes {
		instructions = instructions[:mcpMaxInstructionBytes]
	}

	return MCPProbeResult{
		OK:           true,
		Stage:        "list",
		ServerInfo:   initRes.ServerInfo,
		ToolCount:    len(tools),
		Tools:        tools,
		Truncated:    truncated,
		Instructions: instructions,
		DurationMs:   time.Since(started).Milliseconds(),
	}
}

func (s *MCPService) persistServers(servers map[string]MCPServerConfig) error {
	path, err := s.resolvedConfigPath()
	if err != nil {
		return err
	}
	return saveMCPConfigsToFile(path, servers)
}

func (s *MCPService) stopServerRunner(name string) {
	s.mu.Lock()
	runner, ok := s.runners[name]
	if ok {
		delete(s.runners, name)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	runner.cancel()
	if runner.client != nil {
		_ = runner.client.Close()
	}
	select {
	case <-runner.done:
	case <-time.After(mcpCloseTimeout):
	}
}

func (s *MCPService) startServerRunner(cfg MCPServerConfig) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &mcpServerRunner{
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.mu.Lock()
	s.runners[cfg.Name] = runner
	s.mu.Unlock()

	go s.superviseServer(ctx, cfg, runner)
}

func (s *MCPService) superviseServer(ctx context.Context, cfg MCPServerConfig, runner *mcpServerRunner) {
	defer close(runner.done)

	policy := cfg.Reconnect
	if policy == nil {
		policy = &MCPReconnectConfig{
			Enabled:        true,
			InitialDelayMs: mcpDefaultReconnectInitMs,
			MaxDelayMs:     mcpDefaultReconnectMaxMs,
			MaxAttempts:    mcpDefaultReconnectAttempts,
		}
	}

	failedAttempts := 0
	for {
		if ctx.Err() != nil {
			return
		}

		// Stdio runtime LookPath preflight — missing runtime stops immediately without burning retry budget.
		var resolvedExec, resolvedPATH string
		if cfg.Transport == MCPTransportStdio {
			resolvedPATH = resolveLoginShellPATH()
			rt := checkMCPRuntimeWithPATH(cfg.Command, resolvedPATH)
			if !rt.Found {
				s.updateServerStatus(cfg.Name, func(st *MCPServerStatus) {
					st.State = MCPStateMissingRuntime
					st.LastError = rt.Message
					st.RuntimeHint = &rt
					st.Tools = nil
					st.ToolCount = 0
					st.Instructions = ""
					st.NextRetryAtMs = 0
				})
				return
			}
			resolvedExec = rt.ResolvedPath
		}

		s.updateServerStatus(cfg.Name, func(st *MCPServerStatus) {
			st.State = MCPStateConnecting
			st.Attempt = failedAttempts
			st.MaxAttempts = policy.MaxAttempts
			st.NextRetryAtMs = 0
			st.RuntimeHint = nil
		})

		var wire mcpWireClient
		var err error
		var syncMu sync.Mutex
		onToolsChanged := func() {
			syncMu.Lock()
			defer syncMu.Unlock()
			if ctx.Err() != nil || wire == nil {
				return
			}
			syncCtx, syncCancel := context.WithTimeout(ctx, 15*time.Second)
			defer syncCancel()
			rawTools, _, listErr := wire.ListTools(syncCtx, mcpProbeMaxPages, mcpProbeMaxTools)
			if listErr != nil {
				log.Printf("mcp(%s): tool re-sync failed: %v", cfg.Name, redactMCPSecrets(listErr.Error(), cfg))
				return
			}
			tools, mapErr := mapWireTools(cfg.Name, rawTools)
			if mapErr != nil {
				log.Printf("mcp(%s): tool re-sync validation failed: %v", cfg.Name, mapErr)
				return
			}
			s.updateServerStatus(cfg.Name, func(st *MCPServerStatus) {
				st.Tools = tools
				st.ToolCount = len(tools)
			})
		}

		if cfg.Transport == MCPTransportStdio {
			wire, err = startMCPStdioClient(cfg, resolvedExec, resolvedPATH, onToolsChanged)
		} else {
			wire = newMCPHTTPClient(cfg, onToolsChanged, s.httpTransport)
		}

		var connectedAt time.Time
		if err == nil {
			s.mu.Lock()
			if s.runners[cfg.Name] == runner {
				runner.client = wire
			}
			s.mu.Unlock()

			initCtx, initCancel := context.WithTimeout(ctx, 20*time.Second)
			initRes, initErr := wire.Initialize(initCtx)
			initCancel()
			if initErr != nil {
				err = initErr
			} else {
				listCtx, listCancel := context.WithTimeout(ctx, 20*time.Second)
				rawTools, _, listErr := wire.ListTools(listCtx, mcpProbeMaxPages, mcpProbeMaxTools)
				listCancel()
				if listErr != nil {
					err = listErr
				} else {
					tools, mapErr := mapWireTools(cfg.Name, rawTools)
					if mapErr != nil {
						err = mapErr
					} else {
						instructions := strings.TrimSpace(initRes.Instructions)
						if len(instructions) > mcpMaxInstructionBytes {
							instructions = instructions[:mcpMaxInstructionBytes]
						}
						connectedAt = time.Now()
						s.updateServerStatus(cfg.Name, func(st *MCPServerStatus) {
							st.State = MCPStateConnected
							st.ServerInfo = initRes.ServerInfo
							st.Instructions = instructions
							st.Tools = tools
							st.ToolCount = len(tools)
							st.LastError = ""
							st.RuntimeHint = nil
							st.Attempt = 0
							st.NextRetryAtMs = 0
							st.ConnectedAtMs = connectedAt.UnixMilli()
						})

						// Wait until context is canceled or transport dies.
						select {
						case <-ctx.Done():
							_ = wire.Close()
							return
						case <-wire.Done():
							err = fmt.Errorf("connection closed")
						}
					}
				}
			}
			_ = wire.Close()
			s.mu.Lock()
			if s.runners[cfg.Name] == runner && runner.client == wire {
				runner.client = nil
			}
			s.mu.Unlock()
		}

		if ctx.Err() != nil {
			return
		}

		errText := redactMCPSecrets(err.Error(), cfg)

		// Fatal config / auth errors (invalid URL, HTTP 401/403, initial 404) do NOT burn reconnect attempts.
		if isFatalMCPConnectError(err) {
			s.updateServerStatus(cfg.Name, func(st *MCPServerStatus) {
				st.State = MCPStateError
				st.LastError = errText
				st.Tools = nil
				st.ToolCount = 0
				st.Instructions = ""
				st.NextRetryAtMs = 0
			})
			return
		}

		// Reset attempt budget if the connection stayed healthy past the stability window (maxDelayMs).
		if !connectedAt.IsZero() && time.Since(connectedAt) >= time.Duration(policy.MaxDelayMs)*time.Millisecond {
			failedAttempts = 0
		}

		if !policy.Enabled {
			s.updateServerStatus(cfg.Name, func(st *MCPServerStatus) {
				st.State = MCPStateError
				st.LastError = errText
				st.Tools = nil
				st.ToolCount = 0
				st.NextRetryAtMs = 0
			})
			return
		}

		failedAttempts++
		if failedAttempts > policy.MaxAttempts {
			s.updateServerStatus(cfg.Name, func(st *MCPServerStatus) {
				st.State = MCPStateError
				st.LastError = fmt.Sprintf("gave up after %d failed reconnect attempts: %s", policy.MaxAttempts, errText)
				st.Tools = nil
				st.ToolCount = 0
				st.Instructions = ""
				st.NextRetryAtMs = 0
				st.Attempt = policy.MaxAttempts
				st.MaxAttempts = policy.MaxAttempts
			})
			return
		}

		delayMs := policy.InitialDelayMs
		for i := 1; i < failedAttempts; i++ {
			delayMs *= 2
			if delayMs >= policy.MaxDelayMs {
				delayMs = policy.MaxDelayMs
				break
			}
		}
		if delayMs > policy.MaxDelayMs {
			delayMs = policy.MaxDelayMs
		}
		nextRetry := time.Now().Add(time.Duration(delayMs) * time.Millisecond)
		s.updateServerStatus(cfg.Name, func(st *MCPServerStatus) {
			st.State = MCPStateReconnecting
			st.LastError = errText
			st.Attempt = failedAttempts
			st.MaxAttempts = policy.MaxAttempts
			st.NextRetryAtMs = nextRetry.UnixMilli()
		})

		timer := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *MCPService) updateServerStatus(name string, mutate func(*MCPServerStatus)) {
	s.mu.Lock()
	st, ok := s.statuses[name]
	if !ok {
		cfg, cfgOK := s.servers[name]
		if !cfgOK {
			s.mu.Unlock()
			return
		}
		st = MCPServerStatus{
			Name:      cfg.Name,
			Transport: cfg.Transport,
			Enabled:   cfg.Enabled,
		}
	}
	mutate(&st)
	s.statuses[name] = st
	s.rebuildToolIndexLocked()
	s.mu.Unlock()

	s.emitSnapshot()
}

func (s *MCPService) rebuildToolIndexLocked() {
	idx := make(map[string]mcpIndexedTool)
	for serverName, st := range s.statuses {
		if st.State != MCPStateConnected {
			continue
		}
		for _, tool := range st.Tools {
			idx[tool.PublicName] = mcpIndexedTool{
				serverName: serverName,
				rawName:    tool.RawName,
				tool:       tool,
			}
		}
	}
	s.toolIndex = idx
}

func mapWireTools(serverName string, rawTools []mcpWireTool) ([]MCPToolInfo, error) {
	loaderName := mcpLoaderPublicName(serverName)
	seenPublic := make(map[string]string, len(rawTools))
	tools := make([]MCPToolInfo, 0, len(rawTools))

	for _, rt := range rawTools {
		if strings.TrimSpace(rt.Name) == "" {
			continue
		}
		pub := mcpPublicToolName(serverName, rt.Name)
		if pub == loaderName {
			log.Printf("mcp(%s): skipping server tool %q because it collides with on-demand loader %q", serverName, rt.Name, loaderName)
			continue
		}
		if prevRaw, dup := seenPublic[pub]; dup {
			return nil, fmt.Errorf("duplicate MCP tool name %q (collides with %q -> %q)", rt.Name, prevRaw, pub)
		}
		seenPublic[pub] = rt.Name

		schema := rt.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		readOnly := rt.Annotations != nil && rt.Annotations.ReadOnlyHint != nil && *rt.Annotations.ReadOnlyHint

		tools = append(tools, MCPToolInfo{
			PublicName:   pub,
			RawName:      rt.Name,
			ServerName:   serverName,
			Description:  rt.Description,
			InputSchema:  schema,
			ReadOnlyHint: readOnly,
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].RawName < tools[j].RawName
	})
	return tools, nil
}

// ============================================================================
// ToolService Integration (Policy Classification & Execution)
// ============================================================================

func (s *MCPService) resolveLoaderServer(publicName string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name, cfg := range s.servers {
		if cfg.Enabled && mcpLoaderPublicName(name) == publicName {
			return name, true
		}
	}
	return "", false
}

// classifyMCPToolCall implements Vela's 3-tier permission model for MCP tools:
//   - mcp__<server>__load_tools: local read-only meta-tool -> always allow
//   - read-only mode: allow only when annotations.readOnlyHint == true, else deny
//   - workspace-write mode: allow when readOnlyHint == true or server.TrustServer == true, else ask
//   - full-access mode: allow
func (s *MCPService) classifyMCPToolCall(req ToolExecuteRequest, perm string) ToolClassifyResult {
	if _, isLoader := s.resolveLoaderServer(req.ToolName); isLoader {
		return ToolClassifyResult{Decision: DecisionAllow}
	}

	s.mu.RLock()
	indexed, ok := s.toolIndex[req.ToolName]
	var trustServer bool
	if ok {
		trustServer = s.servers[indexed.serverName].TrustServer
	}
	s.mu.RUnlock()

	if !ok {
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   fmt.Sprintf("MCP tool %q is not available or its server is disconnected", req.ToolName),
		}
	}

	switch perm {
	case PermissionReadOnly:
		if indexed.tool.ReadOnlyHint {
			return ToolClassifyResult{Decision: DecisionAllow}
		}
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   fmt.Sprintf("MCP tool %s (%s) does not declare readOnlyHint=true and cannot run in read-only mode", indexed.rawName, indexed.serverName),
		}
	case PermissionWorkspaceWrite:
		if indexed.tool.ReadOnlyHint || trustServer {
			return ToolClassifyResult{Decision: DecisionAllow}
		}
		return ToolClassifyResult{
			Decision:        DecisionAsk,
			Reason:          fmt.Sprintf("MCP tool %q on server %q requires approval in workspace-write mode", indexed.rawName, indexed.serverName),
			SessionAllowKey: fmt.Sprintf("mcp:%s:%s", indexed.serverName, indexed.rawName),
		}
	case PermissionFullAccess:
		return ToolClassifyResult{Decision: DecisionAllow}
	default:
		return ToolClassifyResult{
			Decision: DecisionAsk,
			Reason:   fmt.Sprintf("MCP tool %q on server %q requires approval", indexed.rawName, indexed.serverName),
		}
	}
}

func (s *MCPService) executeMCPTool(ctx context.Context, req ToolExecuteRequest) (ToolExecuteResult, error) {
	if serverName, isLoader := s.resolveLoaderServer(req.ToolName); isLoader {
		return s.executeLoadTools(serverName, req.Input)
	}

	s.mu.RLock()
	indexed, ok := s.toolIndex[req.ToolName]
	cfg := s.servers[indexed.serverName]
	runner := s.runners[indexed.serverName]
	var wire mcpWireClient
	if runner != nil {
		wire = runner.client
	}
	s.mu.RUnlock()

	if !ok || wire == nil {
		return ToolExecuteResult{
			ModelContent: fmt.Sprintf("MCP_SERVER_UNAVAILABLE: server for tool %q is not connected", req.ToolName),
			IsError:      true,
		}, nil
	}

	timeoutMs := cfg.ToolCallTimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = mcpDefaultToolCallTimeoutMs
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	res, err := wire.CallTool(callCtx, indexed.rawName, req.Input)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ToolExecuteResult{
				ModelContent: "ABORTED_BEFORE_DISPATCH: MCP tool call cancelled by user.",
				IsError:      true,
			}, nil
		}
		// If a Streamable HTTP server restarted and invalidated Mcp-Session-Id (runtime HTTP 404),
		// close the stale generation so the supervisor reconnects immediately.
		if isSessionExpiredMCPError(err) {
			_ = wire.Close()
		}
		redactedErr := redactMCPSecrets(err.Error(), cfg)
		return ToolExecuteResult{
			ModelContent: fmt.Sprintf("MCP tool %q failed: %s", indexed.rawName, redactedErr),
			IsError:      true,
			UIMeta: map[string]any{
				"mcpServer": indexed.serverName,
				"mcpTool":   indexed.rawName,
			},
		}, nil
	}

	text := formatMCPCallResult(res, indexed.rawName)
	if res.IsError {
		text = redactMCPSecrets(text, cfg)
	}

	return ToolExecuteResult{
		ModelContent: text,
		IsError:      res.IsError,
		UIMeta: map[string]any{
			"mcpServer": indexed.serverName,
			"mcpTool":   indexed.rawName,
		},
	}, nil
}

func (s *MCPService) executeLoadTools(serverName string, input map[string]any) (ToolExecuteResult, error) {
	rawList, ok := input["tools"]
	if !ok || rawList == nil {
		return ToolExecuteResult{
			ModelContent: fmt.Sprintf("Error: %s requires a non-empty \"tools\" array of tool names.", mcpLoaderPublicName(serverName)),
			IsError:      true,
		}, nil
	}

	var requested []string
	switch v := rawList.(type) {
	case []any:
		for _, item := range v {
			if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
				requested = append(requested, strings.TrimSpace(str))
			}
		}
	case []string:
		for _, str := range v {
			if strings.TrimSpace(str) != "" {
				requested = append(requested, strings.TrimSpace(str))
			}
		}
	}
	if len(requested) == 0 {
		return ToolExecuteResult{
			ModelContent: fmt.Sprintf("Error: %s requires at least one tool name.", mcpLoaderPublicName(serverName)),
			IsError:      true,
		}, nil
	}

	s.mu.RLock()
	st, exists := s.statuses[serverName]
	s.mu.RUnlock()
	if !exists || st.State != MCPStateConnected {
		return ToolExecuteResult{
			ModelContent: fmt.Sprintf("MCP_SERVER_UNAVAILABLE: MCP server %q is not connected.", serverName),
			IsError:      true,
		}, nil
	}

	offered := make(map[string]MCPToolInfo, len(st.Tools))
	for _, t := range st.Tools {
		offered[t.RawName] = t
	}

	var unknown []string
	var loadedPublic []string
	var loadedRaw []string
	seen := make(map[string]bool)
	for _, name := range requested {
		if seen[name] {
			continue
		}
		seen[name] = true
		tool, found := offered[name]
		if !found {
			unknown = append(unknown, fmt.Sprintf("%q", name))
			continue
		}
		loadedRaw = append(loadedRaw, tool.RawName)
		loadedPublic = append(loadedPublic, tool.PublicName)
	}

	if len(unknown) > 0 {
		return ToolExecuteResult{
			ModelContent: fmt.Sprintf("MCP server %s does not offer %s; use names from its on-demand tool list.", serverName, strings.Join(unknown, ", ")),
			IsError:      true,
		}, nil
	}

	return ToolExecuteResult{
		ModelContent: fmt.Sprintf("Loaded: %s.", strings.Join(loadedPublic, ", ")),
		IsError:      false,
		UIMeta: map[string]any{
			"mcpServer":      serverName,
			"mcpTool":        mcpLoaderRawName,
			"mcpLoadedTools": loadedRaw,
		},
	}, nil
}
