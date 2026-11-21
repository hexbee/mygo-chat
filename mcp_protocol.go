package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	mcpClientName         = "vela-mcp-client"
	mcpProtocolVersion    = "2025-06-18"
	mcpCloseTimeout       = 3 * time.Second
	mcpStderrRingBytes    = 8 * 1024
	mcpMaxHTTPBodyBytes   = 16 * 1024 * 1024
	mcpMaxInstructionWarn = mcpMaxInstructionBytes
)

// mcpWireClient abstracts a single connected MCP client generation (streamable-http or stdio).
type mcpWireClient interface {
	Initialize(ctx context.Context) (mcpInitializeResult, error)
	ListTools(ctx context.Context, maxPages, maxTools int) ([]mcpWireTool, bool, error)
	CallTool(ctx context.Context, rawName string, arguments map[string]any) (mcpCallToolResult, error)
	Done() <-chan struct{}
	Close() error
}

// mcpFatalConfigError marks non-retryable configuration or auth errors (invalid URL, 401/403, initial 404).
type mcpFatalConfigError struct {
	err error
}

func (e *mcpFatalConfigError) Error() string { return e.err.Error() }
func (e *mcpFatalConfigError) Unwrap() error { return e.err }

// mcpSessionExpiredError marks a runtime HTTP 404 on an established Mcp-Session-Id,
// signaling that the remote server restarted and the session should be re-initialized.
type mcpSessionExpiredError struct {
	statusCode int
	detail     string
}

func (e *mcpSessionExpiredError) Error() string {
	if e.detail != "" {
		return fmt.Sprintf("MCP session expired (HTTP %d): %s", e.statusCode, e.detail)
	}
	return fmt.Sprintf("MCP session expired (HTTP %d)", e.statusCode)
}

func isFatalMCPConnectError(err error) bool {
	var fatal *mcpFatalConfigError
	return errors.As(err, &fatal)
}

func isSessionExpiredMCPError(err error) bool {
	var expired *mcpSessionExpiredError
	return errors.As(err, &expired)
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *jsonRPCError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type mcpInitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools json.RawMessage `json:"tools,omitempty"`
	} `json:"capabilities"`
	ServerInfo   *MCPServerInfo `json:"serverInfo,omitempty"`
	Instructions string         `json:"instructions,omitempty"`
}

type mcpWireToolAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

type mcpWireTool struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	InputSchema map[string]any          `json:"inputSchema,omitempty"`
	Annotations *mcpWireToolAnnotations `json:"annotations,omitempty"`
}

type mcpListToolsResult struct {
	Tools      []mcpWireTool `json:"tools"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type mcpContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Name     string `json:"name,omitempty"`
	URI      string `json:"uri,omitempty"`
}

type mcpCallToolResult struct {
	Content           []mcpContentBlock `json:"content"`
	StructuredContent any               `json:"structuredContent,omitempty"`
	IsError           bool              `json:"isError,omitempty"`
}

// formatMCPCallResult projects MCP content blocks into concise model-facing text,
// matching deepseek-harness's text/resource_link/placeholder projection rules.
func formatMCPCallResult(res mcpCallToolResult, rawName string) string {
	var parts []string
	for _, block := range res.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case "resource_link":
			if block.Name == "" || block.URI == "" {
				parts = append(parts, "[resource link unavailable: missing name or URI]")
			} else {
				parts = append(parts, fmt.Sprintf("Resource link: %s (%s)", block.Name, block.URI))
			}
		case "image":
			mime := block.MimeType
			if mime == "" {
				mime = "unknown media type"
			}
			parts = append(parts, fmt.Sprintf("[image content: %s; inline image projection is not supported]", mime))
		case "audio":
			mime := block.MimeType
			if mime == "" {
				mime = "unknown media type"
			}
			parts = append(parts, fmt.Sprintf("[audio content unsupported: %s]", mime))
		case "resource":
			parts = append(parts, "[embedded resource unsupported]")
		default:
			if block.Type != "" {
				parts = append(parts, fmt.Sprintf("[unsupported MCP content type: %s]", block.Type))
			}
		}
	}
	if len(parts) == 0 && res.StructuredContent != nil {
		if b, err := json.MarshalIndent(res.StructuredContent, "", "  "); err == nil {
			parts = append(parts, string(b))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("(%s returned no model-visible content)", rawName)
	}
	return strings.Join(parts, "\n")
}

// ============================================================================
// Streamable HTTP MCP Client
// ============================================================================

type mcpHTTPClient struct {
	cfg            MCPServerConfig
	httpClient     *http.Client
	onToolsChanged func()

	mu              sync.RWMutex
	sessionID       string
	protocolVersion string
	initialized     bool
	hasToolsCap     bool
	closed          bool

	nextID atomic.Uint64
	doneCh chan struct{}
}

func newMCPHTTPClient(cfg MCPServerConfig, onToolsChanged func(), transport http.RoundTripper) *mcpHTTPClient {
	client := &http.Client{}
	if transport != nil {
		client.Transport = transport
	}
	return &mcpHTTPClient{
		cfg:            cfg,
		httpClient:     client,
		onToolsChanged: onToolsChanged,
		doneCh:         make(chan struct{}),
	}
}

func (c *mcpHTTPClient) Done() <-chan struct{} { return c.doneCh }

func (c *mcpHTTPClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	sid := c.sessionID
	protoVer := c.protocolVersion
	close(c.doneCh)
	c.mu.Unlock()

	// Best-effort session termination per Streamable HTTP spec.
	if sid != "" {
		ctx, cancel := context.WithTimeout(context.Background(), mcpCloseTimeout)
		defer cancel()
		if req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.cfg.URL, nil); err == nil {
			for k, v := range c.cfg.Headers {
				req.Header.Set(k, v)
			}
			req.Header.Set("Mcp-Session-Id", sid)
			if protoVer != "" {
				req.Header.Set("MCP-Protocol-Version", protoVer)
			}
			if resp, err := c.httpClient.Do(req); err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
			}
		}
	}
	return nil
}

func (c *mcpHTTPClient) postRPC(ctx context.Context, payload jsonRPCRequest, expectResponse bool, isHandshake bool) (*jsonRPCMessage, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, &mcpFatalConfigError{err: fmt.Errorf("invalid URL: %w", err)}
	}
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	c.mu.RLock()
	sid := c.sessionID
	protoVer := c.protocolVersion
	c.mu.RUnlock()
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	if protoVer != "" {
		req.Header.Set("MCP-Protocol-Version", protoVer)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if newSID := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id")); newSID != "" {
		c.mu.Lock()
		c.sessionID = newSID
		c.mu.Unlock()
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		snippet := readBoundedSnippet(resp.Body, 512)
		return nil, &mcpFatalConfigError{err: fmt.Errorf("HTTP %d authentication failed%s", resp.StatusCode, formatSnippetSuffix(snippet))}
	}
	if resp.StatusCode == http.StatusNotFound {
		snippet := readBoundedSnippet(resp.Body, 512)
		if isHandshake || sid == "" {
			return nil, &mcpFatalConfigError{err: fmt.Errorf("HTTP 404 endpoint not found%s", formatSnippetSuffix(snippet))}
		}
		return nil, &mcpSessionExpiredError{statusCode: resp.StatusCode, detail: snippet}
	}
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		if !expectResponse {
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected HTTP %d for request %s", resp.StatusCode, payload.Method)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := readBoundedSnippet(resp.Body, 512)
		return nil, fmt.Errorf("HTTP %d%s", resp.StatusCode, formatSnippetSuffix(snippet))
	}

	if !expectResponse {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, nil
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		return c.readSSEResponse(resp.Body, payload.ID)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, mcpMaxHTTPBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed reading HTTP response: %w", err)
	}
	var msg jsonRPCMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC response: %w", err)
	}
	if msg.Error != nil {
		return nil, msg.Error
	}
	return &msg, nil
}

func (c *mcpHTTPClient) readSSEResponse(body io.Reader, expectedID any) (*jsonRPCMessage, error) {
	reader := bufio.NewReaderSize(body, 64*1024)
	var dataLines []string
	expectedIDStr := fmt.Sprint(expectedID)

	flushEvent := func() (*jsonRPCMessage, error) {
		if len(dataLines) == 0 {
			return nil, nil
		}
		raw := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		if raw == "" || raw == "[DONE]" {
			return nil, nil
		}
		var msg jsonRPCMessage
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			return nil, nil
		}
		if msg.Method == "notifications/tools/list_changed" && c.onToolsChanged != nil {
			go c.onToolsChanged()
			return nil, nil
		}
		if len(msg.ID) > 0 && normalizeJSONID(msg.ID) == expectedIDStr {
			if msg.Error != nil {
				return nil, msg.Error
			}
			return &msg, nil
		}
		return nil, nil
	}

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				if msg, fErr := flushEvent(); msg != nil || fErr != nil {
					return msg, fErr
				}
			} else if strings.HasPrefix(trimmed, "data:") {
				val := strings.TrimPrefix(trimmed, "data:")
				val = strings.TrimPrefix(val, " ")
				dataLines = append(dataLines, val)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if msg, fErr := flushEvent(); msg != nil || fErr != nil {
					return msg, fErr
				}
				return nil, fmt.Errorf("SSE stream ended without matching JSON-RPC response")
			}
			return nil, err
		}
	}
}

func (c *mcpHTTPClient) Initialize(ctx context.Context) (mcpInitializeResult, error) {
	id := c.nextID.Add(1)
	msg, err := c.postRPC(ctx, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    mcpClientName,
				"version": CurrentAppVersion,
			},
		},
	}, true, true)
	if err != nil {
		return mcpInitializeResult{}, err
	}

	var initRes mcpInitializeResult
	if err := json.Unmarshal(msg.Result, &initRes); err != nil {
		return mcpInitializeResult{}, fmt.Errorf("invalid initialize result: %w", err)
	}
	proto := strings.TrimSpace(initRes.ProtocolVersion)
	if proto == "" {
		proto = mcpProtocolVersion
	}

	c.mu.Lock()
	c.protocolVersion = proto
	c.initialized = true
	c.hasToolsCap = len(initRes.Capabilities.Tools) > 0 && string(initRes.Capabilities.Tools) != "null"
	c.mu.Unlock()

	// Send notifications/initialized (best-effort; ignore 202/204).
	if _, err := c.postRPC(ctx, jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}, false, false); err != nil {
		return mcpInitializeResult{}, fmt.Errorf("notifications/initialized failed: %w", err)
	}

	return initRes, nil
}

func (c *mcpHTTPClient) ListTools(ctx context.Context, maxPages, maxTools int) ([]mcpWireTool, bool, error) {
	c.mu.RLock()
	hasTools := c.hasToolsCap
	c.mu.RUnlock()
	if !hasTools {
		return nil, false, nil
	}

	var all []mcpWireTool
	cursor := ""
	truncated := false
	for page := 0; page < maxPages; page++ {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		id := c.nextID.Add(1)
		msg, err := c.postRPC(ctx, jsonRPCRequest{
			JSONRPC: "2.0",
			ID:      id,
			Method:  "tools/list",
			Params:  params,
		}, true, false)
		if err != nil {
			return nil, false, err
		}
		var res mcpListToolsResult
		if err := json.Unmarshal(msg.Result, &res); err != nil {
			return nil, false, fmt.Errorf("invalid tools/list result: %w", err)
		}
		for _, t := range res.Tools {
			if len(all) >= maxTools {
				truncated = true
				break
			}
			all = append(all, t)
		}
		if truncated || res.NextCursor == "" {
			break
		}
		if page == maxPages-1 {
			truncated = true
			break
		}
		cursor = res.NextCursor
	}
	return all, truncated, nil
}

func (c *mcpHTTPClient) CallTool(ctx context.Context, rawName string, arguments map[string]any) (mcpCallToolResult, error) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	id := c.nextID.Add(1)
	msg, err := c.postRPC(ctx, jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      rawName,
			"arguments": arguments,
		},
	}, true, false)
	if err != nil {
		return mcpCallToolResult{}, err
	}
	var res mcpCallToolResult
	if err := json.Unmarshal(msg.Result, &res); err != nil {
		return mcpCallToolResult{}, fmt.Errorf("invalid tools/call result: %w", err)
	}
	return res, nil
}

// ============================================================================
// Stdio MCP Client
// ============================================================================

type mcpStdioClient struct {
	cfg            MCPServerConfig
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stderrBuf      *ringBuffer
	onToolsChanged func()

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan mcpRPCResponse
	closed  bool
	hasCap  bool

	nextID atomic.Uint64
	doneCh chan struct{}
}

type mcpRPCResponse struct {
	msg *jsonRPCMessage
	err error
}

func startMCPStdioClient(cfg MCPServerConfig, resolvedExecutable string, resolvedPATH string, onToolsChanged func()) (*mcpStdioClient, error) {
	cmd := exec.Command(resolvedExecutable, cfg.Args...)
	cmd.Env = buildMCPStdioEnv(cfg.Env, resolvedPATH)
	if cfg.Cwd != "" {
		cmd.Dir = expandHomePath(cfg.Cwd)
	}
	configureProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to open stdio stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("failed to open stdio stdout: %w", err)
	}
	stderrBuf := newRingBuffer(mcpStderrRingBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("failed to start stdio server %q: %w", cfg.Command, err)
	}

	client := &mcpStdioClient{
		cfg:            cfg,
		cmd:            cmd,
		stdin:          stdin,
		stderrBuf:      stderrBuf,
		onToolsChanged: onToolsChanged,
		pending:        make(map[string]chan mcpRPCResponse),
		doneCh:         make(chan struct{}),
	}

	go client.readLoop(stdout)
	return client, nil
}

func (c *mcpStdioClient) Done() <-chan struct{} { return c.doneCh }

func (c *mcpStdioClient) readLoop(stdout io.ReadCloser) {
	defer func() {
		_ = stdout.Close()
		waitErr := c.cmd.Wait()
		stderrText := strings.TrimSpace(c.recentStderr())
		closeErr := fmt.Errorf("stdio MCP server exited")
		if waitErr != nil {
			closeErr = fmt.Errorf("stdio MCP server exited: %v", waitErr)
		}
		if stderrText != "" {
			closeErr = fmt.Errorf("%s (stderr: %s)", closeErr.Error(), stderrText)
		}

		c.mu.Lock()
		if !c.closed {
			c.closed = true
			close(c.doneCh)
		}
		for id, ch := range c.pending {
			ch <- mcpRPCResponse{err: closeErr}
			delete(c.pending, id)
		}
		c.mu.Unlock()
	}()

	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 {
				var msg jsonRPCMessage
				if jErr := json.Unmarshal(trimmed, &msg); jErr == nil {
					if msg.Method == "notifications/tools/list_changed" && c.onToolsChanged != nil {
						go c.onToolsChanged()
					} else if len(msg.ID) > 0 {
						idStr := normalizeJSONID(msg.ID)
						c.mu.Lock()
						ch, ok := c.pending[idStr]
						if ok {
							delete(c.pending, idStr)
						}
						c.mu.Unlock()
						if ok {
							if msg.Error != nil {
								ch <- mcpRPCResponse{err: msg.Error}
							} else {
								msgCopy := msg
								ch <- mcpRPCResponse{msg: &msgCopy}
							}
						}
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (c *mcpStdioClient) recentStderr() string {
	raw, _, _ := c.stderrBuf.readFrom(0)
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) > 1024 {
		trimmed = trimmed[len(trimmed)-1024:]
	}
	return trimmed
}

func (c *mcpStdioClient) writeMessage(payload jsonRPCRequest) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(data)
	return err
}

func (c *mcpStdioClient) request(ctx context.Context, method string, params any) (*jsonRPCMessage, error) {
	id := c.nextID.Add(1)
	idStr := strconv.FormatUint(id, 10)
	ch := make(chan mcpRPCResponse, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("stdio MCP server is closed")
	}
	c.pending[idStr] = ch
	c.mu.Unlock()

	if err := c.writeMessage(jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		c.mu.Lock()
		delete(c.pending, idStr)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp.msg, resp.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, idStr)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *mcpStdioClient) Initialize(ctx context.Context) (mcpInitializeResult, error) {
	msg, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    mcpClientName,
			"version": CurrentAppVersion,
		},
	})
	if err != nil {
		return mcpInitializeResult{}, err
	}
	var initRes mcpInitializeResult
	if err := json.Unmarshal(msg.Result, &initRes); err != nil {
		return mcpInitializeResult{}, fmt.Errorf("invalid initialize result: %w", err)
	}
	c.mu.Lock()
	c.hasCap = len(initRes.Capabilities.Tools) > 0 && string(initRes.Capabilities.Tools) != "null"
	c.mu.Unlock()

	if err := c.writeMessage(jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}); err != nil {
		return mcpInitializeResult{}, fmt.Errorf("notifications/initialized failed: %w", err)
	}
	return initRes, nil
}

func (c *mcpStdioClient) ListTools(ctx context.Context, maxPages, maxTools int) ([]mcpWireTool, bool, error) {
	c.mu.Lock()
	hasTools := c.hasCap
	c.mu.Unlock()
	if !hasTools {
		return nil, false, nil
	}

	var all []mcpWireTool
	cursor := ""
	truncated := false
	for page := 0; page < maxPages; page++ {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		msg, err := c.request(ctx, "tools/list", params)
		if err != nil {
			return nil, false, err
		}
		var res mcpListToolsResult
		if err := json.Unmarshal(msg.Result, &res); err != nil {
			return nil, false, fmt.Errorf("invalid tools/list result: %w", err)
		}
		for _, t := range res.Tools {
			if len(all) >= maxTools {
				truncated = true
				break
			}
			all = append(all, t)
		}
		if truncated || res.NextCursor == "" {
			break
		}
		if page == maxPages-1 {
			truncated = true
			break
		}
		cursor = res.NextCursor
	}
	return all, truncated, nil
}

func (c *mcpStdioClient) CallTool(ctx context.Context, rawName string, arguments map[string]any) (mcpCallToolResult, error) {
	if arguments == nil {
		arguments = map[string]any{}
	}
	msg, err := c.request(ctx, "tools/call", map[string]any{
		"name":      rawName,
		"arguments": arguments,
	})
	if err != nil {
		return mcpCallToolResult{}, err
	}
	var res mcpCallToolResult
	if err := json.Unmarshal(msg.Result, &res); err != nil {
		return mcpCallToolResult{}, fmt.Errorf("invalid tools/call result: %w", err)
	}
	return res, nil
}

func (c *mcpStdioClient) Close() error {
	c.mu.Lock()
	alreadyClosed := c.closed
	c.closed = true
	if !alreadyClosed {
		close(c.doneCh)
	}
	c.mu.Unlock()

	_ = c.stdin.Close()
	killProcessTree(c.cmd)
	return nil
}

func normalizeJSONID(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' {
		var s string
		if err := json.Unmarshal([]byte(trimmed), &s); err == nil {
			return s
		}
	}
	return trimmed
}

func readBoundedSnippet(r io.Reader, limit int64) string {
	b, _ := io.ReadAll(io.LimitReader(r, limit))
	return strings.TrimSpace(string(b))
}

func formatSnippetSuffix(snippet string) string {
	if snippet == "" {
		return ""
	}
	return ": " + snippet
}
