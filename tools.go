package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const (
	PermissionReadOnly       = "read-only"
	PermissionWorkspaceWrite = "workspace-write"
	PermissionFullAccess     = "full-access"

	DecisionAllow = "allow"
	DecisionAsk   = "ask"
	DecisionDeny  = "deny"
)

// ToolService exposes Go-native file (and later shell) tools to the frontend via mygo.Bind.
type ToolService struct {
	mu      sync.Mutex
	nextID  uint64
	cancels map[string]map[uint64]context.CancelFunc
}

var globalToolService = &ToolService{
	cancels: make(map[string]map[uint64]context.CancelFunc),
}

// ToolClassifyResult represents the pre-execution policy decision for a tool call.
type ToolClassifyResult struct {
	Decision string `json:"decision"` // "allow" | "ask" | "deny"
	Reason   string `json:"reason"`
}

// ToolFileDiff carries structured before/after text for UI diff rendering.
type ToolFileDiff struct {
	Path    string `json:"path"`
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// ToolExecuteRequest is the unified RPC payload for classifying and executing a client tool.
type ToolExecuteRequest struct {
	SessionID      string         `json:"sessionId"`
	ToolName       string         `json:"toolName"` // read | write | edit | delete_file | glob | grep | bash
	Input          map[string]any `json:"input"`
	Workdir        string         `json:"workdir"`        // User workspace or existing scratchDir; empty triggers lazy scratch on write/edit
	PermissionMode string         `json:"permissionMode"` // read-only | workspace-write | full-access
	Approved       bool           `json:"approved"`       // True only after one-time user approval; backend still re-verifies
}

// ToolExecuteResult separates concise model output from rich UI metadata.
type ToolExecuteResult struct {
	ModelContent    string         `json:"modelContent"`
	IsError         bool           `json:"isError"`
	AssignedScratch string         `json:"assignedScratch,omitempty"`
	UIMeta          map[string]any `json:"uiMeta,omitempty"`
}

func normalizePermissionMode(mode string) string {
	switch mode {
	case PermissionReadOnly, PermissionWorkspaceWrite, PermissionFullAccess:
		return mode
	default:
		return PermissionWorkspaceWrite
	}
}

func isWriteFileTool(name string) bool {
	switch name {
	case "write", "edit", "delete_file":
		return true
	default:
		return false
	}
}

func isReadFileTool(name string) bool {
	switch name {
	case "read", "glob", "grep":
		return true
	default:
		return false
	}
}

func stringInput(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func intInput(m map[string]any, key string, fallback int) int {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok || v == nil {
		return fallback
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return fallback
	}
}

func boolInput(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// ClassifyToolCall performs pre-execution policy enforcement for file tools.
// In Phase 1 M2, this enforces fail-closed hard blocks:
//   - read-only denies all write/edit/delete_file calls and out-of-workspace paths
//   - workspace-write denies out-of-workspace paths (upgraded to ask in M4)
//   - full-access allows cross-directory file operations
func (s *ToolService) ClassifyToolCall(req ToolExecuteRequest) ToolClassifyResult {
	perm := normalizePermissionMode(req.PermissionMode)

	if !isReadFileTool(req.ToolName) && !isWriteFileTool(req.ToolName) {
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   fmt.Sprintf("unsupported tool: %s", req.ToolName),
		}
	}

	if perm == PermissionReadOnly && isWriteFileTool(req.ToolName) {
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   fmt.Sprintf("tool %q is not allowed in read-only mode", req.ToolName),
		}
	}

	allowOutside := perm == PermissionFullAccess
	root, _, err := resolveRootForRequest(req, false)
	if err != nil {
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   err.Error(),
		}
	}

	rawPath := extractTargetPath(req)
	if rawPath == "" {
		// glob and grep default to workspace root when path is omitted.
		if req.ToolName == "glob" || req.ToolName == "grep" {
			if root == "" {
				return ToolClassifyResult{
					Decision: DecisionDeny,
					Reason:   fmt.Sprintf("tool %q requires an active workspace or explicit path", req.ToolName),
				}
			}
			return ToolClassifyResult{Decision: DecisionAllow}
		}
		// Let ExecuteTool return a structured parameter error for missing file_path.
		return ToolClassifyResult{Decision: DecisionAllow}
	}

	followLeaf := req.ToolName != "delete_file"
	_, withinRoot, err := resolveAndValidatePath(rawPath, root, allowOutside, followLeaf)
	if err != nil {
		if errors.Is(err, errPathOutsideWorkspace) || errors.Is(err, errBrokenSymlink) {
			return ToolClassifyResult{
				Decision: DecisionDeny,
				Reason:   err.Error(),
			}
		}
		if root == "" && !allowOutside {
			return ToolClassifyResult{
				Decision: DecisionDeny,
				Reason:   err.Error(),
			}
		}
		// Other path syntax/missing-root errors are surfaced cleanly during ExecuteTool.
		return ToolClassifyResult{Decision: DecisionAllow}
	}

	if !allowOutside && !withinRoot {
		return ToolClassifyResult{
			Decision: DecisionDeny,
			Reason:   fmt.Sprintf("path %q is outside the allowed workspace", rawPath),
		}
	}

	return ToolClassifyResult{Decision: DecisionAllow}
}

func extractTargetPath(req ToolExecuteRequest) string {
	switch req.ToolName {
	case "read", "write", "edit", "delete_file":
		return stringInput(req.Input, "file_path")
	case "glob", "grep":
		return stringInput(req.Input, "path")
	default:
		return ""
	}
}

// ExecuteTool re-verifies policy on the backend (fail-closed) and dispatches the requested tool.
func (s *ToolService) ExecuteTool(req ToolExecuteRequest) (ToolExecuteResult, error) {
	classification := s.ClassifyToolCall(req)
	switch classification.Decision {
	case DecisionDeny:
		return ToolExecuteResult{
			ModelContent: fmt.Sprintf("Permission denied: %s", classification.Reason),
			IsError:      true,
		}, fmt.Errorf("permission denied: %s", classification.Reason)
	case DecisionAsk:
		if !req.Approved {
			return ToolExecuteResult{
				ModelContent: fmt.Sprintf("Approval required: %s", classification.Reason),
				IsError:      true,
			}, fmt.Errorf("approval required: %s", classification.Reason)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	callID := s.registerCancel(req.SessionID, cancel)
	defer s.unregisterCancel(req.SessionID, callID, cancel)

	switch req.ToolName {
	case "read":
		return executeRead(ctx, req)
	case "write":
		return executeWrite(ctx, req)
	case "edit":
		return executeEdit(ctx, req)
	case "delete_file":
		return executeDeleteFile(ctx, req)
	case "glob":
		return executeGlob(ctx, req)
	case "grep":
		return executeGrep(ctx, req)
	default:
		return ToolExecuteResult{
			ModelContent: fmt.Sprintf("Unsupported tool: %s", req.ToolName),
			IsError:      true,
		}, fmt.Errorf("unsupported tool: %s", req.ToolName)
	}
}

// CancelSessionTools cancels all active tool executions belonging to sessionID.
func (s *ToolService) CancelSessionTools(sessionID string) error {
	s.mu.Lock()
	active := s.cancels[sessionID]
	delete(s.cancels, sessionID)
	s.mu.Unlock()

	for _, cancel := range active {
		cancel()
	}
	return nil
}

func (s *ToolService) registerCancel(sessionID string, cancel context.CancelFunc) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancels == nil {
		s.cancels = make(map[string]map[uint64]context.CancelFunc)
	}
	s.nextID++
	id := s.nextID
	if sessionID != "" {
		if s.cancels[sessionID] == nil {
			s.cancels[sessionID] = make(map[uint64]context.CancelFunc)
		}
		s.cancels[sessionID][id] = cancel
	}
	return id
}

func (s *ToolService) unregisterCancel(sessionID string, callID uint64, cancel context.CancelFunc) {
	cancel()
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.cancels[sessionID]; m != nil {
		delete(m, callID)
		if len(m) == 0 {
			delete(s.cancels, sessionID)
		}
	}
}
