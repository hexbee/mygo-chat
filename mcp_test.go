package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMCPPublicToolName(t *testing.T) {
	// Clean case: verbatim
	if got := mcpPublicToolName("github", "list_issues"); got != "mcp__github__list_issues" {
		t.Fatalf("expected mcp__github__list_issues, got %q", got)
	}

	// Lossy normalization: distinct raw names that normalize similarly must not collide
	a := mcpPublicToolName("srv", "foo.bar")
	b := mcpPublicToolName("srv", "foo_bar")
	if a == b {
		t.Fatalf("expected distinct hashed names for foo.bar and foo_bar, both got %q", a)
	}
	if len(a) > mcpMaxPublicNameLen {
		t.Fatalf("name exceeded max length %d: %q (%d)", mcpMaxPublicNameLen, a, len(a))
	}

	// Long raw name: truncated + 12-hex hash, <= 64 chars
	longRaw := strings.Repeat("abcdefghij", 10)
	c := mcpPublicToolName("my-server", longRaw)
	if len(c) > mcpMaxPublicNameLen {
		t.Fatalf("long name exceeded max length %d: %q (%d)", mcpMaxPublicNameLen, c, len(c))
	}
	if mcpPublicToolName("my-server", longRaw) != c {
		t.Fatalf("mcpPublicToolName must be deterministic")
	}
}

func TestMCPRedactSecrets(t *testing.T) {
	cfg := MCPServerConfig{
		Name:      "sec",
		Transport: MCPTransportHTTP,
		URL:       "https://example.com/mcp",
		Headers: map[string]string{
			"Authorization": "Bearer super-secret-token-999",
			"X-API-Key":     "key-abc12345",
		},
		Env: map[string]string{
			"MY_API_SECRET": "env-secret-val",
			"PLAIN_VAR":     "keep-visible",
		},
	}

	raw := "failed with Bearer super-secret-token-999 and token super-secret-token-999, key=key-abc12345, env=env-secret-val, plain=keep-visible"
	redacted := redactMCPSecrets(raw, cfg)
	for _, secret := range []string{"super-secret-token-999", "key-abc12345", "env-secret-val"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q leaked in redacted text: %q", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "keep-visible") {
		t.Fatalf("non-secret env value should not be redacted: %q", redacted)
	}
}

func TestMCPConfigPersistence0600(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, ".vela", "mcp.json")

	input := map[string]MCPServerConfig{
		"remote": {
			Name:      "remote",
			Transport: MCPTransportHTTP,
			Enabled:   true,
			URL:       "https://mcp.example.com/mcp",
			Headers:   map[string]string{"Authorization": "Bearer tok-12345"},
		},
	}
	if err := saveMCPConfigsToFile(cfgPath, input); err != nil {
		t.Fatalf("saveMCPConfigsToFile failed: %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(cfgPath)
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("expected 0600 permissions on mcp.json, got %o", perm)
		}
	}

	loaded, err := loadMCPConfigsFromFile(cfgPath)
	if err != nil {
		t.Fatalf("loadMCPConfigsFromFile failed: %v", err)
	}
	got, ok := loaded["remote"]
	if !ok {
		t.Fatalf("expected server 'remote' in loaded config")
	}
	if got.ToolLoading != MCPToolLoadingOnDemand {
		t.Fatalf("expected default toolLoading=on-demand, got %q", got.ToolLoading)
	}
	if got.ToolCallTimeoutMs != mcpDefaultToolCallTimeoutMs {
		t.Fatalf("expected default timeout %d, got %d", mcpDefaultToolCallTimeoutMs, got.ToolCallTimeoutMs)
	}
	if got.Headers["Authorization"] != "Bearer tok-12345" {
		t.Fatalf("header mismatch: %v", got.Headers)
	}
}

func TestMCPImportConfigJSON(t *testing.T) {
	validJSON := `{
		"mcpServers": {
			"docs": {
				"url": "https://docs.example.com/mcp",
				"headers": { "X-API-Key": "secret-123" }
			},
			"local-fs": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
				"disabled": true
			}
		}
	}`
	parsed, verrs := parseMCPServersImportJSON(validJSON)
	if len(verrs) != 0 {
		t.Fatalf("expected valid JSON import, got errors: %+v", verrs)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(parsed))
	}
	if parsed[0].Name != "docs" || parsed[0].Transport != MCPTransportHTTP || !parsed[0].Enabled {
		t.Fatalf("unexpected docs server: %+v", parsed[0])
	}
	if parsed[1].Name != "local-fs" || parsed[1].Transport != MCPTransportStdio || parsed[1].Enabled {
		t.Fatalf("unexpected local-fs server: %+v", parsed[1])
	}

	invalidJSON := `{
		"mcpServers": {
			"bad name!": { "url": "ftp://invalid" },
			"empty-stdio": { "transport": "stdio" }
		}
	}`
	_, verrs = parseMCPServersImportJSON(invalidJSON)
	if len(verrs) < 3 {
		t.Fatalf("expected field-level validation errors, got: %+v", verrs)
	}
}

func TestMCPRuntimePreflightAndEnvScrub(t *testing.T) {
	emptyDir := t.TempDir()

	npxCheck := checkMCPRuntimeWithPATH("npx", emptyDir)
	if npxCheck.Found || npxCheck.Kind != "npx" || !strings.Contains(npxCheck.Message, "Node.js") {
		t.Fatalf("expected missing npx hint, got: %+v", npxCheck)
	}

	uvxCheck := checkMCPRuntimeWithPATH("uvx", emptyDir)
	if uvxCheck.Found || uvxCheck.Kind != "uvx" || !strings.Contains(uvxCheck.Message, "uv") {
		t.Fatalf("expected missing uvx hint, got: %+v", uvxCheck)
	}

	// Verify env scrubbing strips parent secrets but keeps explicit server env
	t.Setenv("TEST_PARENT_SECRET_TOKEN", "should-be-stripped")
	env := buildMCPStdioEnv(map[string]string{"EXPLICIT_API_KEY": "allowed-explicit"}, "/custom/bin")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "TEST_PARENT_SECRET_TOKEN") {
		t.Fatalf("parent secret env var was not stripped")
	}
	if !strings.Contains(joined, "EXPLICIT_API_KEY=allowed-explicit") {
		t.Fatalf("explicit server env var missing")
	}
	if !strings.Contains(joined, "PATH=/custom/bin") {
		t.Fatalf("resolved PATH missing")
	}
}

func waitForMCPState(t *testing.T, svc *MCPService, name string, wantState string, timeout time.Duration) MCPServerStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snap := svc.GetSnapshot()
		if st, ok := snap.Statuses[name]; ok && st.State == wantState {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := svc.GetSnapshot()
	t.Fatalf("timed out waiting for server %q to reach state %q (current: %+v)", name, wantState, snap.Statuses[name])
	return MCPServerStatus{}
}

type roundTripHandler struct {
	handler http.Handler
}

func (rt roundTripHandler) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rt.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func TestMCPStreamableHTTP_EndToEnd_JSON_And_SSE(t *testing.T) {
	const authToken = "Bearer test-http-secret-xyz"
	var listCalls atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Header.Get("Authorization") != authToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
			return
		}
		var req struct {
			JSONRPC string         `json:"jsonrpc"`
			ID      any            `json:"id"`
			Method  string         `json:"method"`
			Params  map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "sess-1")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2025-06-18",
					"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
					"serverInfo":      map[string]any{"name": "mock-http-mcp", "version": "1.2.3"},
					"instructions":    "Use search_docs before mutating state.",
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			listCalls.Add(1)
			cursor, _ := req.Params["cursor"].(string)
			w.Header().Set("Content-Type", "application/json")
			if cursor == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"tools": []map[string]any{
							{
								"name":        "search_docs",
								"description": "Search documentation",
								"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
								"annotations": map[string]any{"readOnlyHint": true},
							},
							{
								"name":        "load_tools",
								"description": "Colliding tool name should be skipped",
								"inputSchema": map[string]any{"type": "object"},
							},
						},
						"nextCursor": "page-2",
					},
				})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"tools": []map[string]any{
							{
								"name":        "create_issue",
								"description": "Create an issue",
								"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}},
							},
						},
					},
				})
			}
		case "tools/call":
			toolName, _ := req.Params["name"].(string)
			args, _ := req.Params["arguments"].(map[string]any)
			switch toolName {
			case "search_docs":
				// Respond using text/event-stream (SSE)
				w.Header().Set("Content-Type", "text/event-stream")
				payload, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"content": []map[string]any{
							{"type": "text", "text": fmt.Sprintf("SSE result for %v", args["q"])},
						},
					},
				})
				_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
			case "create_issue":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"isError": true,
						"content": []map[string]any{
							{"type": "text", "text": "Upstream rejected token test-http-secret-xyz"},
						},
					},
				})
			}
		}
	})

	cfgPath := filepath.Join(t.TempDir(), "mcp.json")
	svc := newMCPService(cfgPath)
	svc.emitFn = func(MCPSnapshot) {}
	svc.httpTransport = roundTripHandler{handler: handler}
	defer svc.StopAll()

	cfg := MCPServerConfig{
		Name:      "demo",
		Transport: MCPTransportHTTP,
		Enabled:   true,
		URL:       "https://mock-mcp.local/mcp",
		Headers:   map[string]string{"Authorization": authToken},
	}

	// 1. ProbeServer must be side-effect-free and list paginated tools (skipping load_tools collision)
	probe := svc.ProbeServer(cfg)
	if !probe.OK || probe.ToolCount != 2 || probe.ServerInfo == nil || probe.ServerInfo.Name != "mock-http-mcp" {
		t.Fatalf("unexpected probe result: %+v", probe)
	}
	if len(svc.GetSnapshot().Servers) != 0 {
		t.Fatalf("ProbeServer must not register server in live snapshot")
	}

	// 2. UpsertServer connects and populates tools & instructions
	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}
	st := waitForMCPState(t, svc, "demo", MCPStateConnected, 5*time.Second)
	if st.ToolCount != 2 || st.Instructions != "Use search_docs before mutating state." {
		t.Fatalf("unexpected connected status: %+v", st)
	}

	// 3. 3-tier permission model checks
	loaderName := mcpLoaderPublicName("demo")
	searchName := mcpPublicToolName("demo", "search_docs")
	createName := mcpPublicToolName("demo", "create_issue")

	// load_tools is always allow
	if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: loaderName}, PermissionReadOnly).Decision; dec != DecisionAllow {
		t.Fatalf("expected load_tools allow in read-only, got %s", dec)
	}
	// readOnlyHint=true is allow in read-only and workspace-write
	if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: searchName}, PermissionReadOnly).Decision; dec != DecisionAllow {
		t.Fatalf("expected readOnlyHint tool allow in read-only, got %s", dec)
	}
	// mutating tool is deny in read-only, ask in workspace-write, allow in full-access
	if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: createName}, PermissionReadOnly).Decision; dec != DecisionDeny {
		t.Fatalf("expected mutating MCP tool deny in read-only, got %s", dec)
	}
	if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: createName}, PermissionWorkspaceWrite).Decision; dec != DecisionAsk {
		t.Fatalf("expected mutating MCP tool ask in workspace-write, got %s", dec)
	}
	if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: createName}, PermissionFullAccess).Decision; dec != DecisionAllow {
		t.Fatalf("expected mutating MCP tool allow in full-access, got %s", dec)
	}

	// 4. load_tools validation and execution
	loadRes, err := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
		ToolName: loaderName,
		Input:    map[string]any{"tools": []any{"search_docs"}},
	})
	if err != nil || loadRes.IsError || !strings.Contains(loadRes.ModelContent, searchName) {
		t.Fatalf("unexpected load_tools result: %+v, err=%v", loadRes, err)
	}
	badLoadRes, _ := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
		ToolName: loaderName,
		Input:    map[string]any{"tools": []any{"nonexistent"}},
	})
	if !badLoadRes.IsError {
		t.Fatalf("expected error for unknown tool in load_tools")
	}

	// 5. SSE tool call execution
	callRes, err := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
		ToolName: searchName,
		Input:    map[string]any{"q": "vela"},
	})
	if err != nil || callRes.IsError || callRes.ModelContent != "SSE result for vela" {
		t.Fatalf("unexpected SSE tool call result: %+v, err=%v", callRes, err)
	}

	// 6. isError response redacts configured header token
	errRes, err := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
		ToolName: createName,
		Input:    map[string]any{"title": "bug"},
	})
	if err != nil || !errRes.IsError {
		t.Fatalf("expected isError=true tool result, got: %+v, err=%v", errRes, err)
	}
	if strings.Contains(errRes.ModelContent, "test-http-secret-xyz") || !strings.Contains(errRes.ModelContent, mcpRedactedPlaceholder) {
		t.Fatalf("expected secret token to be redacted in isError output, got: %q", errRes.ModelContent)
	}

	// 7. TrustServer skips approval in workspace-write mode
	cfg.TrustServer = true
	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer(TrustServer=true) failed: %v", err)
	}
	waitForMCPState(t, svc, "demo", MCPStateConnected, 5*time.Second)
	if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: createName}, PermissionWorkspaceWrite).Decision; dec != DecisionAllow {
		t.Fatalf("expected trusted server to allow mutating tool in workspace-write, got %s", dec)
	}
}

func TestMCPStreamableHTTP_FatalErrorsDoNotRetry(t *testing.T) {
	var hits atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid bearer token"))
	})

	svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
	svc.emitFn = func(MCPSnapshot) {}
	svc.httpTransport = roundTripHandler{handler: handler}
	defer svc.StopAll()

	cfg := MCPServerConfig{
		Name:      "authfail",
		Transport: MCPTransportHTTP,
		Enabled:   true,
		URL:       "https://authfail.local/mcp",
		Reconnect: &MCPReconnectConfig{
			Enabled:        true,
			InitialDelayMs: 50,
			MaxDelayMs:     200,
			MaxAttempts:    5,
		},
	}
	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}
	st := waitForMCPState(t, svc, "authfail", MCPStateError, 3*time.Second)
	time.Sleep(150 * time.Millisecond)
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected fatal HTTP 401 to stop after 1 attempt without retrying, got %d attempts (state=%+v)", got, st)
	}
}

// TestMCPStdioHelperProcess acts as a lightweight in-binary stdio MCP server when invoked by TestMCPStdio_EndToEnd_Subprocess.
func TestMCPStdioHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_STDIO_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		var resp map[string]any
		switch req.Method {
		case "initialize":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2025-06-18",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "mock-stdio-mcp", "version": "0.1.0"},
				},
			}
		case "notifications/initialized":
			continue
		case "tools/list":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "echo",
							"description": "Echo input text",
							"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}},
							"annotations": map[string]any{"readOnlyHint": true},
						},
					},
				},
			}
		case "tools/call":
			args, _ := req.Params["arguments"].(map[string]any)
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": fmt.Sprintf("echo:%v", args["msg"])},
					},
				},
			}
		}
		if resp != nil {
			out, _ := json.Marshal(resp)
			_, _ = fmt.Fprintf(os.Stdout, "%s\n", out)
		}
	}
	os.Exit(0)
}

func TestMCPStdio_EndToEnd_Subprocess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
	svc.emitFn = func(MCPSnapshot) {}
	defer svc.StopAll()

	cfg := MCPServerConfig{
		Name:      "local-echo",
		Transport: MCPTransportStdio,
		Enabled:   true,
		Command:   exe,
		Args:      []string{"-test.run=^TestMCPStdioHelperProcess$"},
		Env:       map[string]string{"GO_WANT_MCP_STDIO_HELPER": "1"},
	}

	probe := svc.ProbeServer(cfg)
	if !probe.OK || probe.ToolCount != 1 || probe.ServerInfo == nil || probe.ServerInfo.Name != "mock-stdio-mcp" {
		t.Fatalf("unexpected stdio probe result: %+v", probe)
	}

	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer stdio failed: %v", err)
	}
	st := waitForMCPState(t, svc, "local-echo", MCPStateConnected, 5*time.Second)
	if st.ToolCount != 1 {
		t.Fatalf("expected 1 stdio tool, got %+v", st)
	}

	echoPub := mcpPublicToolName("local-echo", "echo")
	callRes, err := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
		ToolName: echoPub,
		Input:    map[string]any{"msg": "hello-mcp"},
	})
	if err != nil || callRes.IsError || callRes.ModelContent != "echo:hello-mcp" {
		t.Fatalf("unexpected stdio call result: %+v, err=%v", callRes, err)
	}

	if _, err := svc.DeleteServer("local-echo"); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}
	if len(svc.GetSnapshot().Servers) != 0 {
		t.Fatalf("expected empty servers after delete")
	}
}
