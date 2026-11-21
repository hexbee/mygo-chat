package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// mcpClientName identifies Vela to MCP servers during the initialize handshake.
	mcpClientName = "vela-mcp-client"

	// mcpProtocolVersion pins the handshake to the 2025-06-18 revision.
	//
	// Two reasons. First, Vela's supervisor owns session lifetime, while the SDK's
	// default 2026-07-28 path negotiates a stateless transport through
	// server/discover. Second, that discovery POST may legitimately return HTTP 404
	// on legacy servers before the SDK falls back to initialize, which would look
	// exactly like a fatal "endpoint not found" handshake failure.
	mcpProtocolVersion = "2025-06-18"

	// mcpCloseTimeout bounds a single generation teardown (HTTP session DELETE or
	// stdio process-tree cleanup).
	mcpCloseTimeout = 3 * time.Second
	// mcpStderrRingBytes bounds the retained stderr tail used for stdio diagnostics.
	mcpStderrRingBytes = 8 * 1024
	// mcpHTTPErrorSnippetBytes bounds the response body kept from a failed HTTP call.
	mcpHTTPErrorSnippetBytes = 512
	// mcpMaxJSONResponseBytes bounds one JSON-RPC response body so a misbehaving
	// server cannot exhaust memory. SSE streams are bounded per event by the SDK
	// (MaxEventSize) instead, so only JSON framing is capped here.
	mcpMaxJSONResponseBytes = 16 * 1024 * 1024
	// mcpStdioTerminateGrace is how long a stdio server may take to exit after its
	// stdin closes before the SDK escalates to SIGTERM and then SIGKILL.
	mcpStdioTerminateGrace = 1 * time.Second
	// mcpStdioWaitDelay bounds exec.Cmd.Wait when a wrapper or grandchild keeps the
	// stderr pipe open, so a single misbehaving server cannot pin the supervisor.
	mcpStdioWaitDelay = 2 * time.Second
	// mcpDefaultKeepAlive probes idle supervised sessions so silently dropped HTTP
	// or proxy connections are detected before the next tool call. Only valid for
	// protocol versions < 2026-07-28 (which matches mcpProtocolVersion).
	mcpDefaultKeepAlive = 30 * time.Second
	// mcpDefaultKeepAliveFailureThreshold tolerates one transient ping blip before
	// closing the session.
	mcpDefaultKeepAliveFailureThreshold = 2
)

// mcpWireClient abstracts a single connected MCP client generation (streamable-http or stdio).
//
// The transport/protocol layer is implemented by the official
// github.com/modelcontextprotocol/go-sdk/mcp package; this interface is the seam
// MCPService uses for supervision, so supervision, backoff and the atomic tool
// snapshot are independent of the wire implementation.
type mcpWireClient interface {
	Initialize(ctx context.Context) (mcpInitializeResult, error)
	ListTools(ctx context.Context, maxPages, maxTools int) ([]mcpWireTool, bool, error)
	CallTool(ctx context.Context, rawName string, arguments map[string]any) (mcpCallToolResult, error)
	Done() <-chan struct{}
	WaitError() error
	Close() error
	CloseWithCause(cause error) error
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

// isGenericMCPCloseError reports whether err is a bare mcp.ErrConnectionClosed
// without additional diagnostic context (such as an attached stdio stderr tail).
func isGenericMCPCloseError(err error) bool {
	return err != nil &&
		errors.Is(err, mcp.ErrConnectionClosed) &&
		strings.TrimSpace(err.Error()) == mcp.ErrConnectionClosed.Error()
}

// mcpInitializeResult is the SDK initialize result projected onto the only fields
// MCPService consumes: server identity and instructions. Tools capability is kept
// internally because it gates tools/list.
type mcpInitializeResult struct {
	ServerInfo   *MCPServerInfo
	Instructions string
}

type mcpWireTool struct {
	Name         string
	Description  string
	InputSchema  map[string]any
	ReadOnlyHint bool
}

// mcpContentBlock is the transport-independent projection of one MCP content
// block. formatMCPCallResult turns these into concise model-facing text.
type mcpContentBlock struct {
	Type     string
	Text     string
	MimeType string
	Name     string
	URI      string
}

type mcpCallToolResult struct {
	Content           []mcpContentBlock
	StructuredContent any
	IsError           bool
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
			if block.URI == "" {
				parts = append(parts, "[embedded resource unsupported]")
				break
			}
			label := block.URI
			if block.MimeType != "" {
				label += "; " + block.MimeType
			}
			parts = append(parts, fmt.Sprintf("[embedded resource: %s; inline projection is not supported]", label))
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
// Per-call HTTP status capture
// ============================================================================

// mcpCallStatus records the HTTP status of the requests that belong to exactly
// one wire-level call.
//
// The SDK intentionally flattens transport failures into wrapped errors (a 401
// becomes `sending "initialize": Unauthorized`), which is not enough to tell a
// fatal auth failure from a transient network error. The status is therefore
// captured in the RoundTripper.
//
// It is scoped per call (carried in the call context) rather than per session:
// the frontend runs up to 4 read-only MCP tool calls concurrently, so a shared
// "last status" slot would let one call's 200 hide another call's 404 — or let
// one call's 404 turn an unrelated local timeout into a bogus session expiry.
type mcpCallStatus struct {
	mu       sync.Mutex
	status   int
	detail   string
	observed bool
}

func (s *mcpCallStatus) record(status int, detail string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.detail, s.observed = status, detail, true
}

func (s *mcpCallStatus) result() (status int, detail string, observed bool) {
	if s == nil {
		return 0, "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.detail, s.observed
}

type mcpCallStatusKey struct{}

// withMCPCallStatus tags ctx so the transport can attribute responses to this call.
func withMCPCallStatus(ctx context.Context, status *mcpCallStatus) context.Context {
	return context.WithValue(ctx, mcpCallStatusKey{}, status)
}

func mcpCallStatusFromContext(ctx context.Context) *mcpCallStatus {
	status, _ := ctx.Value(mcpCallStatusKey{}).(*mcpCallStatus)
	return status
}

// mcpHeaderTransport injects the statically configured headers (Authorization,
// X-API-Key, ...) into every request, bounds JSON bodies and records the
// resulting HTTP status against the call that issued the request.
type mcpHeaderTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (rt *mcpHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	outReq := req
	if len(rt.headers) > 0 {
		// Clone so static headers never leak into the caller's request or into a
		// shared transport's connection metadata.
		outReq = req.Clone(req.Context())
		for k, v := range rt.headers {
			if mcpReservedHeader(k) {
				continue
			}
			outReq.Header.Set(k, v)
		}
	}

	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(outReq)
	if err != nil {
		return nil, err
	}

	// Only the JSON-RPC POSTs carry call status; the standalone SSE GET and the
	// session DELETE are best-effort and must not influence classification.
	if req.Method == http.MethodPost {
		detail := ""
		if resp.StatusCode >= http.StatusBadRequest {
			detail = readResponseSnippet(resp, mcpHTTPErrorSnippetBytes)
		}
		mcpCallStatusFromContext(req.Context()).record(resp.StatusCode, detail)
	}

	if isJSONContentType(resp.Header.Get("Content-Type")) {
		resp.Body = &mcpBoundedBody{ReadCloser: resp.Body, remaining: mcpMaxJSONResponseBytes}
	}
	return resp, nil
}

func isJSONContentType(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "application/json")
}

// errMCPResponseTooLarge fails a call whose JSON body exceeds the cap instead of
// truncating it, so the caller sees a transport error rather than a parse error.
var errMCPResponseTooLarge = errors.New("MCP response body exceeds the 16 MiB cap")

// mcpBoundedBody applies a hard byte budget to a response body.
type mcpBoundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *mcpBoundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		// A body of exactly the budget must still terminate with io.EOF; only one
		// further byte proves the cap was actually exceeded.
		var probe [1]byte
		n, err := b.ReadCloser.Read(probe[:])
		if n > 0 {
			return 0, errMCPResponseTooLarge
		}
		if err == nil {
			err = io.EOF
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

// mcpReservedHeader reports whether a user-configured header must be dropped
// because the SDK owns it. Letting static config rewrite framing or session
// headers would silently break the transport instead of authenticating it.
func mcpReservedHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "accept", "accept-encoding", "connection", "content-length", "content-type",
		"host", "last-event-id", "mcp-protocol-version", "mcp-session-id",
		"transfer-encoding":
		return true
	}
	return false
}

// readResponseSnippet peeks at the head of an error response while leaving the
// body fully readable for the SDK's own error decoding.
func readResponseSnippet(resp *http.Response, limit int64) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	body := resp.Body
	head, _ := io.ReadAll(io.LimitReader(body, limit))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), body), body}
	return strings.TrimSpace(string(head))
}

// ============================================================================
// SDK-backed MCP client (streamable-http and stdio)
// ============================================================================

// mcpSDKClient is one connected generation backed by the official Go SDK. Both
// transports share the same session lifecycle, tool pagination and result
// projection; only transport construction and teardown differ.
type mcpSDKClient struct {
	cfg            MCPServerConfig
	onToolsChanged func()

	client    *mcp.Client
	transport mcp.Transport

	// stdio-only process bookkeeping
	cmd       *exec.Cmd
	stderrBuf *ringBuffer

	mu      sync.Mutex
	session *mcp.ClientSession
	closed  bool

	waitMu  sync.Mutex
	waitErr error

	hasTools atomic.Bool

	doneOnce sync.Once
	doneCh   chan struct{}
}

type mcpRedactedLogWriter struct {
	cfg MCPServerConfig
	out func(string)
}

func (w mcpRedactedLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(redactMCPSecrets(string(p), w.cfg))
	if msg != "" {
		if w.out != nil {
			w.out(msg)
		} else {
			log.Printf("mcp(%s): sdk: %s", w.cfg.Name, msg)
		}
	}
	return len(p), nil
}

// newMCPSDKLogger forwards SDK warnings and errors (e.g. non-405 standalone SSE
// responses or malformed frames) to Vela's log with secrets redacted.
func newMCPSDKLogger(cfg MCPServerConfig, out func(string)) *slog.Logger {
	return slog.New(slog.NewTextHandler(mcpRedactedLogWriter{cfg: cfg, out: out}, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

func newMCPClientBase(cfg MCPServerConfig, onToolsChanged func(), keepAlive time.Duration) *mcpSDKClient {
	c := &mcpSDKClient{
		cfg:            cfg,
		onToolsChanged: onToolsChanged,
		doneCh:         make(chan struct{}),
	}
	opts := &mcp.ClientOptions{
		// Vela exposes no filesystem roots or sampling back to servers; an empty
		// capability set matches the pre-SDK client handshake.
		Capabilities: &mcp.ClientCapabilities{},
		Logger:       newMCPSDKLogger(cfg, nil),
		ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
			if c.onToolsChanged != nil {
				// Never block the SDK read loop while the service re-lists tools.
				go c.onToolsChanged()
			}
		},
	}
	if keepAlive > 0 {
		opts.KeepAlive = keepAlive
		opts.KeepAliveFailureThreshold = mcpDefaultKeepAliveFailureThreshold
	}
	c.client = mcp.NewClient(
		&mcp.Implementation{Name: mcpClientName, Version: CurrentAppVersion},
		opts,
	)
	return c
}

func newMCPHTTPClient(cfg MCPServerConfig, onToolsChanged func(), transport http.RoundTripper) *mcpSDKClient {
	return newMCPHTTPClientWithKeepAlive(cfg, onToolsChanged, transport, 0)
}

func newMCPHTTPClientWithKeepAlive(cfg MCPServerConfig, onToolsChanged func(), transport http.RoundTripper, keepAlive time.Duration) *mcpSDKClient {
	c := newMCPClientBase(cfg, onToolsChanged, keepAlive)

	base := transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.transport = &mcp.StreamableClientTransport{
		Endpoint: cfg.URL,
		HTTPClient: &http.Client{Transport: &mcpHeaderTransport{
			headers: cfg.Headers,
			base:    base,
		}},
	}
	return c
}

// startMCPStdioClient prepares (but does not start) a stdio subprocess. The SDK's
// CommandTransport owns Start/Wait, so the process is only spawned on Initialize.
func startMCPStdioClient(cfg MCPServerConfig, resolvedExecutable string, resolvedPATH string, onToolsChanged func()) (*mcpSDKClient, error) {
	return startMCPStdioClientWithKeepAlive(cfg, resolvedExecutable, resolvedPATH, onToolsChanged, 0)
}

func startMCPStdioClientWithKeepAlive(cfg MCPServerConfig, resolvedExecutable string, resolvedPATH string, onToolsChanged func(), keepAlive time.Duration) (*mcpSDKClient, error) {
	if strings.TrimSpace(resolvedExecutable) == "" {
		return nil, fmt.Errorf("stdio command is empty")
	}

	cmd := exec.Command(resolvedExecutable, cfg.Args...)
	cmd.Env = buildMCPStdioEnv(cfg.Env, resolvedPATH)
	if cfg.Cwd != "" {
		cmd.Dir = expandHomePath(cfg.Cwd)
	}
	cmd.WaitDelay = mcpStdioWaitDelay
	configureProcessGroup(cmd)

	stderrBuf := newRingBuffer(mcpStderrRingBytes)
	cmd.Stderr = stderrBuf

	c := newMCPClientBase(cfg, onToolsChanged, keepAlive)
	c.cmd = cmd
	c.stderrBuf = stderrBuf
	c.transport = &mcp.CommandTransport{
		Command:           cmd,
		TerminateDuration: mcpStdioTerminateGrace,
	}
	return c, nil
}

func (c *mcpSDKClient) Done() <-chan struct{} { return c.doneCh }

func (c *mcpSDKClient) Close() error {
	return c.CloseWithCause(nil)
}

// CloseWithCause closes the generation and records cause as its WaitError when
// the caller already knows why the connection is being torn down (for example a
// runtime HTTP 404 session expiry).
func (c *mcpSDKClient) CloseWithCause(cause error) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	session := c.session
	c.mu.Unlock()

	// Signal the generation end immediately: MCPService's supervisor must not wait
	// on transport teardown (stdio may sit in SIGTERM/SIGKILL escalation).
	c.markDone(cause)

	var closeErr error
	if session != nil {
		// The SDK bounds its own session DELETE, but keep Vela's shutdown budget so a
		// half-open HTTP server cannot stall StopAll.
		done := make(chan error, 1)
		go func() { done <- session.Close() }()
		select {
		case closeErr = <-done:
		case <-time.After(mcpCloseTimeout):
		}
	}

	// The SDK only signals the direct child; npx/uvx wrappers routinely leave
	// grandchildren behind, so reap the whole process group.
	if c.cmd != nil {
		killProcessTree(c.cmd)
	}
	return closeErr
}

func (c *mcpSDKClient) WaitError() error {
	c.waitMu.Lock()
	defer c.waitMu.Unlock()
	return c.waitErr
}

func (c *mcpSDKClient) markDone(waitErr error) {
	c.doneOnce.Do(func() {
		// A nil wait error means a graceful local close; anything else is the
		// transport dying on its own (stdio exit, HTTP session loss) and is worth
		// preserving for the supervisor's LastError as well as logging once.
		if waitErr != nil {
			recorded := c.withStdioStderr(waitErr)
			c.waitMu.Lock()
			c.waitErr = recorded
			c.waitMu.Unlock()
			log.Printf("mcp(%s): connection ended: %s", c.cfg.Name, redactMCPSecrets(recorded.Error(), c.cfg))
		} else if c.stderrBuf != nil {
			if tail := c.recentStderr(); tail != "" {
				log.Printf("mcp(%s): stdio server stderr: %s", c.cfg.Name, redactMCPSecrets(tail, c.cfg))
			}
		}
		close(c.doneCh)
	})
}

func (c *mcpSDKClient) recentStderr() string {
	raw, _, dropped := c.stderrBuf.readFrom(0)
	trimmed := strings.TrimSpace(raw)
	if dropped && trimmed != "" {
		trimmed = "..." + trimmed
	}
	if len(trimmed) > 1024 {
		trimmed = trimmed[len(trimmed)-1024:]
	}
	return trimmed
}

func (c *mcpSDKClient) currentSession() *mcp.ClientSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

// validateEndpoint rejects a malformed streamable-http URL before any request is
// attempted, so the caller can stop at MCPStateError without spending retries.
func (c *mcpSDKClient) validateEndpoint() error {
	if c.cfg.Transport == MCPTransportStdio {
		return nil
	}
	raw := strings.TrimSpace(c.cfg.URL)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return &mcpFatalConfigError{err: fmt.Errorf("invalid URL: %q is not a valid http:// or https:// endpoint", raw)}
	}
	return nil
}

// Initialize performs the MCP handshake (and, for stdio, starts the subprocess).
func (c *mcpSDKClient) Initialize(ctx context.Context) (mcpInitializeResult, error) {
	if err := c.validateEndpoint(); err != nil {
		return mcpInitializeResult{}, err
	}

	call := &mcpCallStatus{}
	session, err := c.client.Connect(
		withMCPCallStatus(ctx, call),
		c.transport,
		&mcp.ClientSessionOptions{ProtocolVersion: mcpProtocolVersion},
	)
	if err != nil {
		return mcpInitializeResult{}, c.classifyConnectError(call, err)
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = session.Close()
		return mcpInitializeResult{}, fmt.Errorf("MCP client is closed")
	}
	c.session = session
	c.mu.Unlock()

	// Detect transport death (stdio exit, HTTP session loss) and release the
	// supervisor so it can rebuild the generation.
	go func() {
		c.markDone(session.Wait())
	}()

	initRes := session.InitializeResult()
	if initRes == nil {
		return mcpInitializeResult{}, fmt.Errorf("MCP server returned an empty initialize result")
	}
	hasTools := initRes.Capabilities != nil && initRes.Capabilities.Tools != nil
	c.hasTools.Store(hasTools)

	out := mcpInitializeResult{Instructions: initRes.Instructions}
	if initRes.ServerInfo != nil {
		out.ServerInfo = &MCPServerInfo{
			Name:    initRes.ServerInfo.Name,
			Version: initRes.ServerInfo.Version,
		}
	}
	return out, nil
}

func (c *mcpSDKClient) ListTools(ctx context.Context, maxPages, maxTools int) ([]mcpWireTool, bool, error) {
	if !c.hasTools.Load() {
		return nil, false, nil
	}
	session := c.currentSession()
	if session == nil {
		return nil, false, fmt.Errorf("MCP session is not initialized")
	}

	call := &mcpCallStatus{}
	ctx = withMCPCallStatus(ctx, call)
	var all []mcpWireTool
	cursor := ""
	truncated := false
	for page := 0; page < maxPages; page++ {
		params := &mcp.ListToolsParams{}
		if cursor != "" {
			params.Cursor = cursor
		}
		res, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, false, c.classifyRuntimeError(call, "tools/list", err)
		}
		for _, tool := range res.Tools {
			if len(all) >= maxTools {
				truncated = true
				break
			}
			all = append(all, wireToolFromSDK(tool))
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

func (c *mcpSDKClient) CallTool(ctx context.Context, rawName string, arguments map[string]any) (mcpCallToolResult, error) {
	session := c.currentSession()
	if session == nil {
		return mcpCallToolResult{}, fmt.Errorf("MCP session is not initialized")
	}
	if arguments == nil {
		arguments = map[string]any{}
	}

	call := &mcpCallStatus{}
	res, err := session.CallTool(withMCPCallStatus(ctx, call), &mcp.CallToolParams{Name: rawName, Arguments: arguments})
	if err != nil {
		return mcpCallToolResult{}, c.classifyRuntimeError(call, "tools/call", err)
	}
	return projectCallToolResult(res), nil
}

// classifyConnectError maps SDK handshake failures onto Vela's fatal/non-fatal split.
func (c *mcpSDKClient) classifyConnectError(call *mcpCallStatus, err error) error {
	status, detail, _ := call.result()
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &mcpFatalConfigError{err: fmt.Errorf("HTTP %d authentication failed%s", status, formatSnippetSuffix(detail))}
	case http.StatusNotFound:
		return &mcpFatalConfigError{err: fmt.Errorf("HTTP 404 endpoint not found%s", formatSnippetSuffix(detail))}
	}
	return c.withStdioStderr(fmt.Errorf("MCP handshake failed: %w", err))
}

// classifyRuntimeError maps a post-handshake failure. A 404 here means the remote
// server restarted and dropped the Mcp-Session-Id, so the caller should close this
// generation and reconnect instead of surfacing a permanent error.
func (c *mcpSDKClient) classifyRuntimeError(call *mcpCallStatus, op string, err error) error {
	if errors.Is(err, mcp.ErrSessionMissing) {
		return &mcpSessionExpiredError{statusCode: http.StatusNotFound, detail: "server no longer recognizes the Mcp-Session-Id"}
	}
	if status, detail, observed := call.result(); observed {
		switch status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return &mcpFatalConfigError{err: fmt.Errorf("HTTP %d authentication failed%s", status, formatSnippetSuffix(detail))}
		case http.StatusNotFound:
			return &mcpSessionExpiredError{statusCode: status, detail: detail}
		}
	}
	return c.withStdioStderr(fmt.Errorf("MCP %s failed: %w", op, err))
}

// withStdioStderr appends the bounded stderr tail to a stdio failure. Without it a
// crashing npx/uvx server only surfaces "handshake failed: EOF", which gives users
// nothing to act on; the outer MCPService redacts the text before display.
func (c *mcpSDKClient) withStdioStderr(err error) error {
	if c.stderrBuf == nil {
		return err
	}
	tail := c.recentStderr()
	if tail == "" {
		return err
	}
	return fmt.Errorf("%w (stderr: %s)", err, tail)
}

// wireToolFromSDK projects an SDK tool definition onto the transport-independent
// shape MCPService's naming/validation logic consumes.
func wireToolFromSDK(tool *mcp.Tool) mcpWireTool {
	if tool == nil {
		return mcpWireTool{}
	}
	return mcpWireTool{
		Name:         tool.Name,
		Description:  tool.Description,
		InputSchema:  toJSONSchemaObject(tool.InputSchema),
		ReadOnlyHint: tool.Annotations != nil && tool.Annotations.ReadOnlyHint,
	}
}

// toJSONSchemaObject normalizes the SDK's `InputSchema any` into the map form the
// tool registry stores and the frontend renders.
func toJSONSchemaObject(schema any) map[string]any {
	switch s := schema.(type) {
	case nil:
		return nil
	case map[string]any:
		return s
	case json.RawMessage:
		var m map[string]any
		if err := json.Unmarshal(s, &m); err == nil {
			return m
		}
		return nil
	default:
		encoded, err := json.Marshal(s)
		if err != nil {
			return nil
		}
		var m map[string]any
		if err := json.Unmarshal(encoded, &m); err != nil {
			return nil
		}
		return m
	}
}

// projectCallToolResult converts SDK content blocks into the projection model.
// Binary payloads are described rather than inlined so tool output stays small.
func projectCallToolResult(res *mcp.CallToolResult) mcpCallToolResult {
	if res == nil {
		return mcpCallToolResult{}
	}
	out := mcpCallToolResult{
		StructuredContent: res.StructuredContent,
		IsError:           res.IsError,
	}
	for _, content := range res.Content {
		switch block := content.(type) {
		case *mcp.TextContent:
			out.Content = append(out.Content, mcpContentBlock{Type: "text", Text: block.Text})
		case *mcp.ResourceLink:
			out.Content = append(out.Content, mcpContentBlock{
				Type:     "resource_link",
				Name:     block.Name,
				URI:      block.URI,
				MimeType: block.MIMEType,
			})
		case *mcp.ImageContent:
			out.Content = append(out.Content, mcpContentBlock{Type: "image", MimeType: block.MIMEType})
		case *mcp.AudioContent:
			out.Content = append(out.Content, mcpContentBlock{Type: "audio", MimeType: block.MIMEType})
		case *mcp.EmbeddedResource:
			projected := mcpContentBlock{Type: "resource"}
			if block.Resource != nil {
				projected.URI = block.Resource.URI
				projected.MimeType = block.Resource.MIMEType
			}
			out.Content = append(out.Content, projected)
		default:
			out.Content = append(out.Content, mcpContentBlock{Type: fmt.Sprintf("%T", content)})
		}
	}
	return out
}

func formatSnippetSuffix(snippet string) string {
	if snippet == "" {
		return ""
	}
	return ": " + snippet
}
