package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpTestAuthHeader = "Bearer test-http-secret-xyz"
	mcpTestAuthToken  = "test-http-secret-xyz"
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

func TestMCPRedactURLSecrets(t *testing.T) {
	cfg := MCPServerConfig{
		Name:      "url-sec",
		Transport: MCPTransportHTTP,
		URL:       "https://user:urlpass-secret@example.com/mcp?api_key=query-secret-42&mode=full",
	}
	raw := "connect https://user:urlpass-secret@example.com/mcp?api_key=query-secret-42&mode=full failed"
	redacted := redactMCPSecrets(raw, cfg)
	for _, secret := range []string{"urlpass-secret", "query-secret-42"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("URL secret %q leaked in redacted text: %q", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "mode=full") {
		t.Fatalf("non-sensitive query key should survive redaction: %q", redacted)
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

func waitForMCPToolCount(t *testing.T, svc *MCPService, name string, want int, timeout time.Duration) MCPServerStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last MCPServerStatus
	for time.Now().Before(deadline) {
		snap := svc.GetSnapshot()
		if st, ok := snap.Statuses[name]; ok {
			last = st
			if st.State == MCPStateConnected && len(st.Tools) == want {
				return st
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for server %q to expose %d tools (current: %+v)", name, want, last)
	return MCPServerStatus{}
}

// roundTripHandler adapts an http.Handler into an offline RoundTripper so MCP
// HTTP behaviour can be exercised through MCPService.httpTransport without
// binding a socket.
type roundTripHandler struct {
	handler http.Handler
}

func (rt roundTripHandler) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rt.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

// mcpFrameRecorder observes the response framing of every MCP request so tests
// can prove whether the SDK actually used JSON or SSE for POST responses.
type mcpFrameRecorder struct {
	handler http.Handler
	mu      sync.Mutex
	frames  map[string]int
}

func newMCPFrameRecorder(handler http.Handler) *mcpFrameRecorder {
	return &mcpFrameRecorder{handler: handler, frames: make(map[string]int)}
}

func (f *mcpFrameRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.handler.ServeHTTP(&mcpFrameWriter{ResponseWriter: w, owner: f, method: r.Method}, r)
}

func (f *mcpFrameRecorder) count(method, contentType string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.frames[method+" "+contentType]
}

func (f *mcpFrameRecorder) observe(method, contentType string) {
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = contentType[:i]
	}
	f.mu.Lock()
	f.frames[method+" "+strings.TrimSpace(contentType)]++
	f.mu.Unlock()
}

// mcpFrameWriter must keep http.Flusher working: the streamable handler streams
// SSE frames through http.ResponseController.
type mcpFrameWriter struct {
	http.ResponseWriter
	owner    *mcpFrameRecorder
	method   string
	recorded bool
}

func (w *mcpFrameWriter) WriteHeader(code int) {
	w.observe()
	w.ResponseWriter.WriteHeader(code)
}

func (w *mcpFrameWriter) Write(p []byte) (int, error) {
	w.observe()
	return w.ResponseWriter.Write(p)
}

func (w *mcpFrameWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *mcpFrameWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *mcpFrameWriter) observe() {
	if w.recorded {
		return
	}
	w.recorded = true
	w.owner.observe(w.method, w.Header().Get("Content-Type"))
}

// ============================================================================
// Real SDK server fixtures
// ============================================================================

func addMCPDemoTools(srv *mcp.Server) {
	srv.AddTool(&mcp.Tool{
		Name:        "search_docs",
		Description: "Search documentation",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"q": map[string]any{"type": "string"}},
		},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		_ = json.Unmarshal(req.Params.Arguments, &args)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("docs result for %v", args["q"])}},
		}, nil
	})

	srv.AddTool(&mcp.Tool{
		Name:        "create_issue",
		Description: "Create an issue",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"title": map[string]any{"type": "string"}},
		},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Upstream echoes a configured secret to prove error text is redacted.
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Upstream rejected token " + mcpTestAuthToken}},
		}, nil
	})

	// A server tool named load_tools must be dropped from the directory so it can
	// never shadow Vela's on-demand loader.
	srv.AddTool(&mcp.Tool{
		Name:        mcpLoaderRawName,
		Description: "Colliding tool name should be skipped",
		InputSchema: map[string]any{"type": "object"},
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "should never be reachable"}}}, nil
	})
}

func newMCPDemoServer(opts *mcp.ServerOptions) *mcp.Server {
	if opts == nil {
		opts = &mcp.ServerOptions{}
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "mock-http-mcp", Version: "1.2.3"}, opts)
	addMCPDemoTools(srv)
	return srv
}

// ============================================================================
// Streamable HTTP end-to-end (JSON and SSE framing, real SDK server + handler)
// ============================================================================

func TestMCPStreamableHTTP_EndToEnd(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		name := "sse"
		if jsonMode {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			srv := newMCPDemoServer(&mcp.ServerOptions{
				Instructions: "Use search_docs before mutating state.",
				// Force multi-page tools/list so cursor pagination is exercised.
				PageSize: 1,
			})
			handler := mcp.NewStreamableHTTPHandler(
				func(*http.Request) *mcp.Server { return srv },
				&mcp.StreamableHTTPOptions{JSONResponse: jsonMode},
			)
			frames := newMCPFrameRecorder(handler)
			httpServer := httptest.NewServer(frames)
			// StopAll must run before the test HTTP server closes, otherwise the
			// long-lived standalone SSE stream keeps Close blocked.
			defer httpServer.Close()

			svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
			svc.emitFn = func(MCPSnapshot) {}
			defer svc.StopAll()

			cfg := MCPServerConfig{
				Name:      "demo",
				Transport: MCPTransportHTTP,
				Enabled:   true,
				URL:       httpServer.URL,
				Headers:   map[string]string{"Authorization": mcpTestAuthHeader},
			}

			// 1. ProbeServer is side-effect free, paginates tools/list (3 pages) and
			//    skips the load_tools collision.
			probe := svc.ProbeServer(cfg)
			if !probe.OK {
				t.Fatalf("unexpected probe failure: %+v", probe)
			}
			if probe.ToolCount != 2 || probe.Truncated {
				t.Fatalf("expected 2 tools and no truncation, got %+v", probe)
			}
			if probe.ServerInfo == nil || probe.ServerInfo.Name != "mock-http-mcp" || probe.ServerInfo.Version != "1.2.3" {
				t.Fatalf("unexpected server info: %+v", probe.ServerInfo)
			}
			if probe.Instructions != "Use search_docs before mutating state." {
				t.Fatalf("unexpected instructions: %q", probe.Instructions)
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
			if st.ServerInfo == nil || st.ServerInfo.Name != "mock-http-mcp" {
				t.Fatalf("unexpected ServerInfo on status: %+v", st.ServerInfo)
			}

			// 3. 3-tier permission model checks
			loaderName := mcpLoaderPublicName("demo")
			searchName := mcpPublicToolName("demo", "search_docs")
			createName := mcpPublicToolName("demo", "create_issue")

			if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: loaderName}, PermissionReadOnly).Decision; dec != DecisionAllow {
				t.Fatalf("expected load_tools allow in read-only, got %s", dec)
			}
			if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: searchName}, PermissionReadOnly).Decision; dec != DecisionAllow {
				t.Fatalf("expected readOnlyHint tool allow in read-only, got %s", dec)
			}
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

			// 5. tools/call (SSE or JSON framed depending on the subtest)
			callRes, err := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
				ToolName: searchName,
				Input:    map[string]any{"q": "vela"},
			})
			if err != nil || callRes.IsError || callRes.ModelContent != "docs result for vela" {
				t.Fatalf("unexpected tool call result: %+v, err=%v", callRes, err)
			}

			// 6. isError response redacts configured header token
			errRes, err := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
				ToolName: createName,
				Input:    map[string]any{"title": "bug"},
			})
			if err != nil || !errRes.IsError {
				t.Fatalf("expected isError=true tool result, got: %+v, err=%v", errRes, err)
			}
			if strings.Contains(errRes.ModelContent, mcpTestAuthToken) || !strings.Contains(errRes.ModelContent, mcpRedactedPlaceholder) {
				t.Fatalf("expected secret token to be redacted in isError output, got: %q", errRes.ModelContent)
			}

			// 6b. The transport really spoke the framing under test.
			wantFrame, otherFrame := "application/json", "text/event-stream"
			if !jsonMode {
				wantFrame, otherFrame = otherFrame, wantFrame
			}
			if got := frames.count(http.MethodPost, wantFrame); got == 0 {
				t.Fatalf("expected POST responses framed as %q, observed %+v", wantFrame, frames.frames)
			}
			if got := frames.count(http.MethodPost, otherFrame); got != 0 {
				t.Fatalf("unexpected %q POST responses in %s mode: %+v", otherFrame, name, frames.frames)
			}

			// 7. Server-pushed notifications/tools/list_changed refreshes the snapshot.
			srv.AddTool(&mcp.Tool{
				Name:        "list_releases",
				Description: "List releases",
				InputSchema: map[string]any{"type": "object"},
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "v0.3.0"}}}, nil
			})
			refreshed := waitForMCPToolCount(t, svc, "demo", 3, 8*time.Second)
			if refreshed.ToolCount != 3 {
				t.Fatalf("expected 3 tools after list_changed, got %+v", refreshed)
			}

			// 8. TrustServer skips approval in workspace-write mode
			cfg.TrustServer = true
			if _, err := svc.UpsertServer(cfg); err != nil {
				t.Fatalf("UpsertServer(TrustServer=true) failed: %v", err)
			}
			waitForMCPState(t, svc, "demo", MCPStateConnected, 5*time.Second)
			if dec := svc.classifyMCPToolCall(ToolExecuteRequest{ToolName: createName}, PermissionWorkspaceWrite).Decision; dec != DecisionAllow {
				t.Fatalf("expected trusted server to allow mutating tool in workspace-write, got %s", dec)
			}
		})
	}
}

// TestMCPListToolsPaginationCaps verifies the maxPages/maxTools guards that keep a
// hostile or broken server from streaming unbounded tool pages.
func TestMCPListToolsPaginationCaps(t *testing.T) {
	srv := newMCPDemoServer(&mcp.ServerOptions{PageSize: 1})
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	cfg := MCPServerConfig{Name: "caps", Transport: MCPTransportHTTP, URL: httpServer.URL}
	wire := newMCPHTTPClient(cfg, nil, nil)
	defer func() { _ = wire.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := wire.Initialize(ctx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	// Page cap: 3 server tools require 3 pages, so 2 pages must report truncation.
	tools, truncated, err := wire.ListTools(ctx, 2, mcpProbeMaxTools)
	if err != nil {
		t.Fatalf("ListTools(maxPages=2) failed: %v", err)
	}
	if len(tools) != 2 || !truncated {
		t.Fatalf("expected 2 tools and truncation with maxPages=2, got %d tools truncated=%v", len(tools), truncated)
	}

	// Tool cap: 2 tools is below the 3 available.
	tools, truncated, err = wire.ListTools(ctx, mcpProbeMaxPages, 2)
	if err != nil {
		t.Fatalf("ListTools(maxTools=2) failed: %v", err)
	}
	if len(tools) != 2 || !truncated {
		t.Fatalf("expected 2 tools and truncation with maxTools=2, got %d tools truncated=%v", len(tools), truncated)
	}

	// Full pagination: all 3 tools, no truncation.
	tools, truncated, err = wire.ListTools(ctx, mcpProbeMaxPages, mcpProbeMaxTools)
	if err != nil {
		t.Fatalf("ListTools(full) failed: %v", err)
	}
	if len(tools) != 3 || truncated {
		t.Fatalf("expected 3 tools without truncation, got %d truncated=%v", len(tools), truncated)
	}
	for _, tool := range tools {
		switch tool.Name {
		case "search_docs":
			if tool.Annotations == nil || tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
				t.Fatalf("search_docs must project readOnlyHint=true: %+v", tool.Annotations)
			}
		case "create_issue":
			// Undeclared annotations must not be reported as read-only.
			if tool.Annotations != nil && tool.Annotations.ReadOnlyHint != nil && *tool.Annotations.ReadOnlyHint {
				t.Fatalf("create_issue must project readOnlyHint=false: %+v", tool.Annotations)
			}
			if tool.InputSchema["type"] != "object" {
				t.Fatalf("expected projected input schema object, got %+v", tool.InputSchema)
			}
		}
	}
}

// TestMCPStreamableHTTP_ConcurrentCallStatusIsolation pins the per-call HTTP
// status scoping. The frontend runs up to 4 read-only MCP tools concurrently, so a
// shared "last status" slot would let one call's 404 turn another call's local
// timeout into a bogus session expiry (and vice versa).
func TestMCPStreamableHTTP_ConcurrentCallStatusIsolation(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "sess-concurrent")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": mcpProtocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "mock-concurrent", "version": "0.0.1"},
				},
			})
		case "tools/call":
			name, _ := req.Params["name"].(string)
			switch name {
			case "slow":
				select {
				case <-time.After(600 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": "slow-ok"}}},
				})
			case "gone":
				// A JSON-RPC error body on 404 makes the SDK surface a plain JSON-RPC
				// error instead of ErrSessionMissing, so classification can only come
				// from this call's own HTTP status.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"error":   map[string]any{"code": -32603, "message": "session gone"},
				})
			default:
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}},
				})
			}
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})

	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	cfg := MCPServerConfig{Name: "concurrent", Transport: MCPTransportHTTP, URL: httpServer.URL}
	wire := newMCPHTTPClient(cfg, nil, nil)
	defer func() { _ = wire.Close() }()

	rootCtx, rootCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer rootCancel()
	if _, err := wire.Initialize(rootCtx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	slowCtx, slowCancel := context.WithTimeout(rootCtx, 150*time.Millisecond)
	defer slowCancel()
	slowDone := make(chan error, 1)
	go func() {
		_, err := wire.CallTool(slowCtx, "slow", nil)
		slowDone <- err
	}()

	// Let the slow call start before the 404 lands, so its timeout would read a
	// stale 404 if the status were shared across calls.
	time.Sleep(30 * time.Millisecond)
	_, goneErr := wire.CallTool(rootCtx, "gone", nil)
	if goneErr == nil {
		t.Fatalf("expected the 404 call to fail")
	}
	if !isSessionExpiredMCPError(goneErr) {
		t.Fatalf("expected the 404 call to be classified as a session expiry, got %v", goneErr)
	}

	slowErr := <-slowDone
	if slowErr == nil {
		t.Fatalf("expected the slow call to fail on its own deadline")
	}
	if isSessionExpiredMCPError(slowErr) {
		t.Fatalf("a concurrent 404 must not turn this call's timeout into a session expiry: %v", slowErr)
	}
	if isFatalMCPConnectError(slowErr) {
		t.Fatalf("a concurrent 404 must not turn this call's timeout into a fatal auth error: %v", slowErr)
	}
	if !errors.Is(slowErr, context.DeadlineExceeded) {
		t.Fatalf("expected the call's own deadline error, got %v", slowErr)
	}

	// The 404 carried a JSON-RPC body, so the session itself is still usable.
	res, err := wire.CallTool(rootCtx, "echo", nil)
	if err != nil || len(res.Content) == 0 || res.Content[0].Text != "ok" {
		t.Fatalf("expected a healthy follow-up call, got %+v err=%v", res, err)
	}
}

// ============================================================================
// Fatal error classification (no retry budget consumed)
// ============================================================================

func TestMCPStreamableHTTP_AuthFailureIsFatal(t *testing.T) {
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
		Headers:   map[string]string{"Authorization": "Bearer nope"},
		Reconnect: &MCPReconnectConfig{
			Enabled:        true,
			InitialDelayMs: 50,
			MaxDelayMs:     200,
			MaxAttempts:    5,
		},
	}

	probe := svc.ProbeServer(cfg)
	if probe.OK || probe.Stage != "connect" || !strings.Contains(probe.Message, "HTTP 401 authentication failed") {
		t.Fatalf("expected fatal 401 probe diagnostics, got %+v", probe)
	}
	if strings.Contains(probe.Message, "Bearer nope") {
		t.Fatalf("probe message leaked the configured credential: %q", probe.Message)
	}

	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}
	st := waitForMCPState(t, svc, "authfail", MCPStateError, 3*time.Second)
	if !strings.Contains(st.LastError, "HTTP 401") {
		t.Fatalf("expected HTTP 401 in LastError, got %+v", st)
	}
	if st.Attempt != 0 || st.NextRetryAtMs != 0 {
		t.Fatalf("fatal auth error must not consume the retry budget: %+v", st)
	}
	time.Sleep(250 * time.Millisecond)
	// Probe + one supervised attempt; the fatal path must never retry.
	if got := hits.Load(); got != 2 {
		t.Fatalf("expected fatal HTTP 401 to stop after probe+1 attempt, got %d requests (state=%+v)", got, st)
	}
}

func TestMCPStreamableHTTP_NotFoundHandshakeIsFatal(t *testing.T) {
	var hits atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no such endpoint"))
	})

	svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
	svc.emitFn = func(MCPSnapshot) {}
	svc.httpTransport = roundTripHandler{handler: handler}
	defer svc.StopAll()

	cfg := MCPServerConfig{
		Name:      "nofound",
		Transport: MCPTransportHTTP,
		Enabled:   true,
		URL:       "https://nofound.local/mcp",
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
	st := waitForMCPState(t, svc, "nofound", MCPStateError, 3*time.Second)
	if !strings.Contains(st.LastError, "HTTP 404") {
		t.Fatalf("expected HTTP 404 in LastError, got %+v", st)
	}
	if st.Attempt != 0 || st.NextRetryAtMs != 0 {
		t.Fatalf("fatal handshake 404 must not consume the retry budget: %+v", st)
	}
	time.Sleep(250 * time.Millisecond)
	if got := hits.Load(); got != 1 {
		t.Fatalf("expected fatal initial 404 to stop after 1 attempt, got %d (state=%+v)", got, st)
	}
}

// TestMCPStreamableHTTP_RuntimeSessionExpiryIsRecoverable proves a post-handshake
// 404 (remote restart invalidated Mcp-Session-Id) is classified as a session
// expiry — reconnectable — instead of a fatal configuration error.
func TestMCPStreamableHTTP_RuntimeSessionExpiryIsRecoverable(t *testing.T) {
	var expired atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if r.Header.Get("Mcp-Session-Id") != "" && expired.Load() {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("session gone"))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-1")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": mcpProtocolVersion,
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "mock-session", "version": "0.0.1"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "echo",
						"description": "Echo",
						"inputSchema": map[string]any{"type": "object"},
					}},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "ok"}},
				},
			})
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})

	cfg := MCPServerConfig{Name: "expiry", Transport: MCPTransportHTTP, URL: "https://expiry.local/mcp"}
	wire := newMCPHTTPClient(cfg, nil, roundTripHandler{handler: handler})
	defer func() { _ = wire.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := wire.Initialize(ctx); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if res, err := wire.CallTool(ctx, "echo", nil); err != nil || res.Content[0].Text != "ok" {
		t.Fatalf("expected successful tool call before expiry, got %+v err=%v", res, err)
	}

	expired.Store(true)
	_, err := wire.CallTool(ctx, "echo", nil)
	if err == nil {
		t.Fatalf("expected an error after the remote dropped the session")
	}
	if !isSessionExpiredMCPError(err) {
		t.Fatalf("expected session-expiry classification, got %v", err)
	}
	if isFatalMCPConnectError(err) {
		t.Fatalf("session expiry must not be classified as fatal: %v", err)
	}

	// The generation must be considered done so the supervisor rebuilds it.
	select {
	case <-wire.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("expected Done() to close after session expiry")
	}
}

func TestMCPStreamableHTTP_InvalidURLIsFatal(t *testing.T) {
	cfg := MCPServerConfig{Name: "badurl", Transport: MCPTransportHTTP, URL: "ftp://example.com/mcp"}
	wire := newMCPHTTPClient(cfg, nil, nil)
	defer func() { _ = wire.Close() }()

	_, err := wire.Initialize(context.Background())
	if err == nil || !isFatalMCPConnectError(err) {
		t.Fatalf("expected fatal invalid-URL classification, got %v", err)
	}
}

// ============================================================================
// stdio end-to-end through a real SDK server subprocess
// ============================================================================

// TestMCPStdioHelperProcess runs as a real MCP stdio server when re-executed by
// TestMCPStdio_EndToEnd_Subprocess.
func TestMCPStdioHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_STDIO_HELPER") != "1" {
		return
	}

	// Simulate a server that dies before the handshake (bad package name, broken
	// dependency, ...) so the client must surface its stderr diagnostics.
	if os.Getenv("MCP_HELPER_FAIL_START") == "1" {
		fmt.Fprintf(os.Stderr, "fatal: provider bootstrap failed (token=%s)\n", os.Getenv("MCP_TEST_PROVIDER_TOKEN"))
		os.Exit(1)
	}

	if pidFile := os.Getenv("MCP_HELPER_PID_FILE"); pidFile != "" {
		record := mcpHelperPIDs{PID: os.Getpid(), Child: spawnMCPHelperGrandchild()}
		if encoded, err := json.Marshal(record); err == nil {
			_ = os.WriteFile(pidFile, encoded, 0o600)
		}
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mock-stdio-mcp", Version: "0.1.0"},
		&mcp.ServerOptions{Instructions: "stdio instructions"},
	)
	srv.AddTool(&mcp.Tool{
		Name:        "echo",
		Description: "Echo input text",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"msg": map[string]any{"type": "string"}},
		},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		_ = json.Unmarshal(req.Params.Arguments, &args)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("echo:%v", args["msg"])}},
		}, nil
	})

	if os.Getenv("MCP_HELPER_ALLOW_DIE") == "1" {
		srv.AddTool(&mcp.Tool{
			Name:        "die",
			Description: "Terminate the server process",
			InputSchema: map[string]any{"type": "object"},
		}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			os.Exit(3)
			return nil, nil
		})
	}

	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "stdio helper server failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// TestMCPStdio_ProcessExitClosesDone proves a crashed stdio server releases the
// supervisor instead of leaving a half-open generation behind.
func TestMCPStdio_ProcessExitClosesDone(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	cfg := MCPServerConfig{
		Name:      "crashy",
		Transport: MCPTransportStdio,
		Command:   exe,
		Args:      []string{"-test.run=^TestMCPStdioHelperProcess$"},
		Env: map[string]string{
			"GO_WANT_MCP_STDIO_HELPER": "1",
			"MCP_HELPER_ALLOW_DIE":     "1",
		},
	}
	wire, err := startMCPStdioClient(cfg, exe, resolveLoginShellPATH(), nil)
	if err != nil {
		t.Fatalf("startMCPStdioClient failed: %v", err)
	}
	defer func() { _ = wire.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	initRes, err := wire.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if initRes.ServerInfo == nil || initRes.ServerInfo.Name != "mock-stdio-mcp" {
		t.Fatalf("unexpected server info: %+v", initRes.ServerInfo)
	}

	// The helper exits its process from inside the handler, so the call itself
	// fails; what matters is that the generation observes the transport death.
	_, _ = wire.CallTool(ctx, "die", nil)

	select {
	case <-wire.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("Done() must close when the stdio subprocess exits")
	}
}

type mcpHelperPIDs struct {
	PID   int `json:"pid"`
	Child int `json:"child,omitempty"`
}

func readMCPHelperPIDs(t *testing.T, path string) mcpHelperPIDs {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			var record mcpHelperPIDs
			if json.Unmarshal(data, &record) == nil && record.PID > 0 {
				return record
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out reading helper pid file %s: %v", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForMCPProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	if pid <= 0 {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !mcpProcessAlive(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process %d is still alive after %s (zombie or leaked subprocess)", pid, timeout)
}

// TestMCPStdio_StderrSurfacedOnHandshakeFailure proves a stdio server that dies
// before the handshake reports its stderr (redacted) instead of a generic EOF.
func TestMCPStdio_StderrSurfacedOnHandshakeFailure(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	const secret = "stderr-secret-abc123"
	svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
	svc.emitFn = func(MCPSnapshot) {}
	defer svc.StopAll()

	cfg := MCPServerConfig{
		Name:      "broken-stdio",
		Transport: MCPTransportStdio,
		Enabled:   true,
		Command:   exe,
		Args:      []string{"-test.run=^TestMCPStdioHelperProcess$"},
		Env: map[string]string{
			"GO_WANT_MCP_STDIO_HELPER": "1",
			"MCP_HELPER_FAIL_START":    "1",
			"MCP_TEST_PROVIDER_TOKEN":  secret,
		},
		Reconnect: &MCPReconnectConfig{Enabled: false},
	}

	probe := svc.ProbeServer(cfg)
	if probe.OK || probe.Stage != "connect" {
		t.Fatalf("expected a connect-stage failure, got %+v", probe)
	}
	if !strings.Contains(probe.Message, "provider bootstrap failed") {
		t.Fatalf("expected stderr diagnostics in the probe message, got %q", probe.Message)
	}
	if strings.Contains(probe.Message, secret) {
		t.Fatalf("stderr diagnostics leaked the configured secret: %q", probe.Message)
	}
	if !strings.Contains(probe.Message, mcpRedactedPlaceholder) {
		t.Fatalf("expected the secret to be redacted in stderr diagnostics: %q", probe.Message)
	}
}

func TestMCPStdio_EndToEnd_Subprocess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	pidFile := filepath.Join(t.TempDir(), "helper-pids.json")
	svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
	svc.emitFn = func(MCPSnapshot) {}
	defer svc.StopAll()

	cfg := MCPServerConfig{
		Name:      "local-echo",
		Transport: MCPTransportStdio,
		Enabled:   true,
		Command:   exe,
		Args:      []string{"-test.run=^TestMCPStdioHelperProcess$"},
		Env: map[string]string{
			"GO_WANT_MCP_STDIO_HELPER": "1",
			"MCP_HELPER_PID_FILE":      pidFile,
		},
	}

	probe := svc.ProbeServer(cfg)
	if !probe.OK || probe.ToolCount != 1 || probe.ServerInfo == nil || probe.ServerInfo.Name != "mock-stdio-mcp" {
		t.Fatalf("unexpected stdio probe result: %+v", probe)
	}
	if probe.Instructions != "stdio instructions" {
		t.Fatalf("unexpected stdio instructions: %q", probe.Instructions)
	}
	// The probe must tear down its own subprocess and process group.
	probePIDs := readMCPHelperPIDs(t, pidFile)
	if runtime.GOOS != "windows" && probePIDs.Child <= 0 {
		t.Fatalf("expected the stdio helper to spawn a grandchild process, got pid %d", probePIDs.Child)
	}
	waitForMCPProcessExit(t, probePIDs.PID, 5*time.Second)
	waitForMCPProcessExit(t, probePIDs.Child, 5*time.Second)

	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer stdio failed: %v", err)
	}
	st := waitForMCPState(t, svc, "local-echo", MCPStateConnected, 5*time.Second)
	if st.ToolCount != 1 {
		t.Fatalf("expected 1 stdio tool, got %+v", st)
	}

	livePIDs := readMCPHelperPIDs(t, pidFile)
	if livePIDs.PID == probePIDs.PID {
		t.Fatalf("expected a fresh stdio generation after UpsertServer, pid %d reused", livePIDs.PID)
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
	// Closing the generation must reap the whole process group, not just the
	// direct child, leaving no zombies behind.
	waitForMCPProcessExit(t, livePIDs.PID, 5*time.Second)
	waitForMCPProcessExit(t, livePIDs.Child, 5*time.Second)
}

// TestMCPStdio_CallToolContentProjection covers every content block type the
// projection layer must handle without leaking binaries into the model context.
func TestMCPStdio_CallToolContentProjection(t *testing.T) {
	res := mcpCallToolResult{
		Content: []mcpContentBlock{
			{Type: "text", Text: "hello"},
			{Type: "resource_link", Name: "spec", URI: "file:///spec.md"},
			{Type: "image", MimeType: "image/png"},
			{Type: "audio", MimeType: "audio/wav"},
			{Type: "resource", URI: "file:///embedded.txt", MimeType: "text/plain"},
			{Type: "mystery"},
		},
	}
	text := formatMCPCallResult(res, "mixed")
	for _, want := range []string{
		"hello",
		"Resource link: spec (file:///spec.md)",
		"[image content: image/png; inline image projection is not supported]",
		"[audio content unsupported: audio/wav]",
		"[embedded resource: file:///embedded.txt; text/plain",
		"[unsupported MCP content type: mystery]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in projected text:\n%s", want, text)
		}
	}

	// Structured content is only surfaced when no content block is visible.
	structured := formatMCPCallResult(mcpCallToolResult{StructuredContent: map[string]any{"ok": true}}, "structured")
	if !strings.Contains(structured, `"ok": true`) {
		t.Fatalf("expected structured content projection, got %q", structured)
	}

	empty := formatMCPCallResult(mcpCallToolResult{}, "nothing")
	if empty != "(nothing returned no model-visible content)" {
		t.Fatalf("unexpected empty projection: %q", empty)
	}
}

// TestMCPWireToolProjectionReadOnly ensures undeclared annotations are treated as
// non-read-only, which is what the read-only permission tier relies on.
func TestMCPWireToolProjectionReadOnly(t *testing.T) {
	ro := wireToolFromSDK(&mcp.Tool{
		Name:        "read_only",
		InputSchema: map[string]any{"type": "object"},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	})
	if ro.Annotations == nil || ro.Annotations.ReadOnlyHint == nil || !*ro.Annotations.ReadOnlyHint {
		t.Fatalf("expected readOnlyHint=true projection, got %+v", ro.Annotations)
	}

	undeclared := wireToolFromSDK(&mcp.Tool{
		Name:        "mutating",
		InputSchema: map[string]any{"type": "object"},
	})
	tools, err := mapWireTools("srv", []mcpWireTool{undeclared})
	if err != nil {
		t.Fatalf("mapWireTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].ReadOnlyHint {
		t.Fatalf("undeclared readOnlyHint must map to false, got %+v", tools)
	}
	if tools[0].InputSchema == nil || tools[0].InputSchema["type"] != "object" {
		t.Fatalf("expected input schema to survive projection: %+v", tools[0].InputSchema)
	}
}

// TestMCPInstructionsTruncation pins the 32 KB instructions budget applied at the
// service boundary, regardless of how much a server sends.
func TestMCPInstructionsTruncation(t *testing.T) {
	longInstructions := strings.Repeat("i", mcpMaxInstructionBytes+4096)
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "verbose", Version: "9.9.9"},
		&mcp.ServerOptions{Instructions: longInstructions, HasTools: true},
	)
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
	svc.emitFn = func(MCPSnapshot) {}
	defer svc.StopAll()

	cfg := MCPServerConfig{Name: "verbose", Transport: MCPTransportHTTP, Enabled: true, URL: httpServer.URL}
	probe := svc.ProbeServer(cfg)
	if !probe.OK {
		t.Fatalf("unexpected probe failure: %+v", probe)
	}
	if len(probe.Instructions) != mcpMaxInstructionBytes {
		t.Fatalf("expected probe instructions capped at %d bytes, got %d", mcpMaxInstructionBytes, len(probe.Instructions))
	}

	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}
	st := waitForMCPState(t, svc, "verbose", MCPStateConnected, 5*time.Second)
	if len(st.Instructions) != mcpMaxInstructionBytes {
		t.Fatalf("expected status instructions capped at %d bytes, got %d", mcpMaxInstructionBytes, len(st.Instructions))
	}
}

// TestMCPNoToolsCapability verifies a server that never advertises tools is
// considered healthy with zero tools instead of triggering tools/list.
func TestMCPNoToolsCapability(t *testing.T) {
	var toolsListCalls atomic.Int32
	srv := mcp.NewServer(&mcp.Implementation{Name: "no-tools", Version: "0.0.1"}, nil)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte(`"tools/list"`)) {
				toolsListCalls.Add(1)
			}
		}
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
	svc.emitFn = func(MCPSnapshot) {}
	defer svc.StopAll()

	cfg := MCPServerConfig{Name: "no-tools", Transport: MCPTransportHTTP, Enabled: true, URL: httpServer.URL}
	probe := svc.ProbeServer(cfg)
	if !probe.OK || probe.ToolCount != 0 {
		t.Fatalf("expected healthy probe with 0 tools, got %+v", probe)
	}
	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}
	st := waitForMCPState(t, svc, "no-tools", MCPStateConnected, 5*time.Second)
	if st.ToolCount != 0 {
		t.Fatalf("expected 0 tools, got %+v", st)
	}
	if got := toolsListCalls.Load(); got != 0 {
		t.Fatalf("expected no tools/list requests without the tools capability, got %d", got)
	}
}

// TestMCPToolResultContentTypes round-trips every content block the SDK can put
// on the wire and checks the model-facing projection stays compact and readable.
func TestMCPToolResultContentTypes(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "content-types", Version: "0.0.2"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "mixed",
		Description: "Returns every content type",
		InputSchema: map[string]any{"type": "object"},
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.TextContent{Text: "hello"},
			&mcp.ResourceLink{URI: "file:///spec.md", Name: "spec"},
			&mcp.ImageContent{MIMEType: "image/png", Data: []byte("not-really-a-png")},
			&mcp.AudioContent{MIMEType: "audio/wav", Data: []byte("not-really-audio")},
			&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{
				URI:      "file:///embedded.txt",
				MIMEType: "text/plain",
				Text:     "embedded body",
			}},
		}}, nil
	})
	srv.AddTool(&mcp.Tool{
		Name:        "structured",
		Description: "Returns structured content only",
		InputSchema: map[string]any{"type": "object"},
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{StructuredContent: map[string]any{"ok": true, "count": 2}}, nil
	})

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{JSONResponse: true},
	)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	svc := newMCPService(filepath.Join(t.TempDir(), "mcp.json"))
	svc.emitFn = func(MCPSnapshot) {}
	defer svc.StopAll()

	cfg := MCPServerConfig{Name: "content", Transport: MCPTransportHTTP, Enabled: true, URL: httpServer.URL}
	if _, err := svc.UpsertServer(cfg); err != nil {
		t.Fatalf("UpsertServer failed: %v", err)
	}
	waitForMCPState(t, svc, "content", MCPStateConnected, 5*time.Second)

	mixed, err := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
		ToolName: mcpPublicToolName("content", "mixed"),
	})
	if err != nil || mixed.IsError {
		t.Fatalf("unexpected mixed content result: %+v err=%v", mixed, err)
	}
	for _, want := range []string{
		"hello",
		"Resource link: spec (file:///spec.md)",
		"[image content: image/png; inline image projection is not supported]",
		"[audio content unsupported: audio/wav]",
		"[embedded resource: file:///embedded.txt; text/plain",
	} {
		if !strings.Contains(mixed.ModelContent, want) {
			t.Fatalf("expected %q in mixed content projection:\n%s", want, mixed.ModelContent)
		}
	}

	structured, err := svc.executeMCPTool(context.Background(), ToolExecuteRequest{
		ToolName: mcpPublicToolName("content", "structured"),
	})
	if err != nil || structured.IsError {
		t.Fatalf("unexpected structured content result: %+v err=%v", structured, err)
	}
	if !strings.Contains(structured.ModelContent, `"ok": true`) || !strings.Contains(structured.ModelContent, `"count": 2`) {
		t.Fatalf("expected structured content JSON, got %q", structured.ModelContent)
	}
}

func TestMCPBoundedResponseBody(t *testing.T) {
	// A body of exactly the budget must terminate with io.EOF, not trip the cap.
	exact := strings.Repeat("a", 8)
	atLimit := &mcpBoundedBody{ReadCloser: io.NopCloser(strings.NewReader(exact)), remaining: int64(len(exact))}
	got, err := io.ReadAll(atLimit)
	if err != nil {
		t.Fatalf("body exactly at the cap must be readable, got %v", err)
	}
	if string(got) != exact {
		t.Fatalf("expected %q, got %q", exact, string(got))
	}

	// One byte over the budget is a transport error, not a truncation.
	overLimit := &mcpBoundedBody{ReadCloser: io.NopCloser(strings.NewReader(exact + "b")), remaining: int64(len(exact))}
	if _, err := io.ReadAll(overLimit); !errors.Is(err, errMCPResponseTooLarge) {
		t.Fatalf("expected response-too-large error for one byte over the cap, got %v", err)
	}

	// Far over the budget is rejected too.
	farOver := &mcpBoundedBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("x", 4096))), remaining: 16}
	if _, err := io.ReadAll(farOver); !errors.Is(err, errMCPResponseTooLarge) {
		t.Fatalf("expected response-too-large error for a far oversized body, got %v", err)
	}

	if !isJSONContentType("application/json; charset=utf-8") {
		t.Fatalf("expected JSON content type with charset to be recognized")
	}
	if isJSONContentType("text/event-stream") {
		t.Fatalf("SSE streams must not be body-capped")
	}
}

func TestMCPErrorClassificationHelpers(t *testing.T) {
	fatal := &mcpFatalConfigError{err: errors.New("HTTP 401 authentication failed")}
	if !isFatalMCPConnectError(fatal) || isSessionExpiredMCPError(fatal) {
		t.Fatalf("fatal config error misclassified")
	}
	expired := &mcpSessionExpiredError{statusCode: 404, detail: "session gone"}
	if isFatalMCPConnectError(expired) || !isSessionExpiredMCPError(expired) {
		t.Fatalf("session expiry misclassified")
	}
	wrapped := fmt.Errorf("wrapped: %w", expired)
	if !isSessionExpiredMCPError(wrapped) {
		t.Fatalf("session expiry must survive error wrapping")
	}
}
