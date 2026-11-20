package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTestWorkspace(t *testing.T) (wsDir string, outsideDir string) {
	t.Helper()
	base := t.TempDir()
	wsDir = filepath.Join(base, "workspace")
	outsideDir = filepath.Join(base, "outside")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	return wsDir, outsideDir
}

func TestReadPaginationAndBinary(t *testing.T) {
	ts := &ToolService{}

	wsDir, _ := setupTestWorkspace(t)
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "line-"+string(rune('a'+i-1)))
	}
	filePath := filepath.Join(wsDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "read",
		Workdir:        wsDir,
		PermissionMode: PermissionReadOnly,
		Input: map[string]any{
			"file_path": "sample.txt",
			"offset":    3,
			"limit":     4,
		},
	})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected non-error read result, got: %s", res.ModelContent)
	}
	if !strings.Contains(res.ModelContent, "     3\tline-c") || !strings.Contains(res.ModelContent, "     6\tline-f") {
		t.Fatalf("unexpected read output:\n%s", res.ModelContent)
	}
	if !strings.Contains(res.ModelContent, "offset=7") {
		t.Fatalf("expected truncation hint with offset=7, got:\n%s", res.ModelContent)
	}
	if truncated, _ := res.UIMeta["truncated"].(bool); !truncated {
		t.Fatalf("expected uiMeta.truncated=true, got %#v", res.UIMeta)
	}

	// Binary file rejection
	binPath := filepath.Join(wsDir, "data.bin")
	if err := os.WriteFile(binPath, []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	binRes, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "read",
		Workdir:        wsDir,
		PermissionMode: PermissionReadOnly,
		Input:          map[string]any{"file_path": "data.bin"},
	})
	if err != nil {
		t.Fatalf("unexpected rpc error for binary file: %v", err)
	}
	if !binRes.IsError || !strings.Contains(binRes.ModelContent, "binary") {
		t.Fatalf("expected binary file error, got: %#v", binRes)
	}
}

func TestWriteNestedDirsAndDiff(t *testing.T) {
	ts := &ToolService{}
	wsDir, _ := setupTestWorkspace(t)

	// Create file in non-existent nested directories (verifies nearest-ancestor path validation).
	res, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "write",
		Workdir:        wsDir,
		PermissionMode: PermissionWorkspaceWrite,
		Input: map[string]any{
			"file_path": "nested/deep/dir/hello.txt",
			"content":   "hello v1\n",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("write nested failed: err=%v res=%#v", err, res)
	}
	if res.AssignedScratch != "" {
		t.Fatalf("expected empty assignedScratch when workdir is set, got %q", res.AssignedScratch)
	}

	// Overwrite and verify diff metadata.
	res2, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "write",
		Workdir:        wsDir,
		PermissionMode: PermissionWorkspaceWrite,
		Input: map[string]any{
			"file_path": "nested/deep/dir/hello.txt",
			"content":   "hello v2\n",
		},
	})
	if err != nil || res2.IsError {
		t.Fatalf("overwrite failed: err=%v res=%#v", err, res2)
	}
	diffs, ok := res2.UIMeta["diffs"].([]ToolFileDiff)
	if !ok || len(diffs) != 1 {
		t.Fatalf("expected 1 ToolFileDiff, got %#v", res2.UIMeta["diffs"])
	}
	if diffs[0].OldText != "hello v1\n" || diffs[0].NewText != "hello v2\n" {
		t.Fatalf("unexpected diff: %#v", diffs[0])
	}
}

func TestEditUniqueAndReplaceAll(t *testing.T) {
	ts := &ToolService{}
	wsDir, _ := setupTestWorkspace(t)
	target := filepath.Join(wsDir, "app.go")
	if err := os.WriteFile(target, []byte("foo\nbar\nfoo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Ambiguous match without replace_all must fail.
	res, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "edit",
		Workdir:        wsDir,
		PermissionMode: PermissionWorkspaceWrite,
		Input: map[string]any{
			"file_path":  "app.go",
			"old_string": "foo",
			"new_string": "baz",
		},
	})
	if err != nil {
		t.Fatalf("unexpected rpc error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.ModelContent, "matched 2 times") {
		t.Fatalf("expected ambiguous match error, got: %#v", res)
	}

	// replace_all=true succeeds and returns structured diff.
	res2, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "edit",
		Workdir:        wsDir,
		PermissionMode: PermissionWorkspaceWrite,
		Input: map[string]any{
			"file_path":   "app.go",
			"old_string":  "foo",
			"new_string":  "baz",
			"replace_all": true,
		},
	})
	if err != nil || res2.IsError {
		t.Fatalf("edit replace_all failed: err=%v res=%#v", err, res2)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "baz\nbar\nbaz\n" {
		t.Fatalf("unexpected file content: %q", string(got))
	}
	diffs, ok := res2.UIMeta["diffs"].([]ToolFileDiff)
	if !ok || len(diffs) != 1 || diffs[0].OldText != "foo\nbar\nfoo\n" || diffs[0].NewText != "baz\nbar\nbaz\n" {
		t.Fatalf("unexpected edit diff: %#v", res2.UIMeta["diffs"])
	}
}

func TestSymlinkEscapeBlocked(t *testing.T) {
	ts := &ToolService{}
	wsDir, outsideDir := setupTestWorkspace(t)

	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Directory symlink pointing outside workspace
	dirLink := filepath.Join(wsDir, "escape_dir")
	if err := os.Symlink(outsideDir, dirLink); err != nil {
		t.Fatalf("symlink dir: %v", err)
	}

	// Dangling symlink pointing to non-existent file outside workspace
	danglingLink := filepath.Join(wsDir, "dangling.txt")
	if err := os.Symlink(filepath.Join(outsideDir, "created_via_dangling.txt"), danglingLink); err != nil {
		t.Fatalf("symlink dangling: %v", err)
	}

	cases := []struct {
		name string
		req  ToolExecuteRequest
	}{
		{
			name: "read through symlink dir",
			req: ToolExecuteRequest{
				SessionID: "s1", ToolName: "read", Workdir: wsDir,
				PermissionMode: PermissionWorkspaceWrite,
				Input:          map[string]any{"file_path": "escape_dir/secret.txt"},
			},
		},
		{
			name: "write new nested file through symlink dir",
			req: ToolExecuteRequest{
				SessionID: "s1", ToolName: "write", Workdir: wsDir,
				PermissionMode: PermissionWorkspaceWrite,
				Input:          map[string]any{"file_path": "escape_dir/newsub/evil.txt", "content": "pwned"},
			},
		},
		{
			name: "write through dangling symlink",
			req: ToolExecuteRequest{
				SessionID: "s1", ToolName: "write", Workdir: wsDir,
				PermissionMode: PermissionWorkspaceWrite,
				Input:          map[string]any{"file_path": "dangling.txt", "content": "pwned"},
			},
		},
		{
			name: "dotdot traversal",
			req: ToolExecuteRequest{
				SessionID: "s1", ToolName: "read", Workdir: wsDir,
				PermissionMode: PermissionWorkspaceWrite,
				Input:          map[string]any{"file_path": "../outside/secret.txt"},
			},
		},
		{
			name: "delete through symlink dir",
			req: ToolExecuteRequest{
				SessionID: "s1", ToolName: "delete_file", Workdir: wsDir,
				PermissionMode: PermissionWorkspaceWrite,
				Input:          map[string]any{"file_path": "escape_dir/secret.txt"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// In read-only mode, all symlink escapes & traversals must be hard-denied even if Approved=true.
			roReq := tc.req
			roReq.PermissionMode = PermissionReadOnly
			if cls := ts.ClassifyToolCall(roReq); cls.Decision != DecisionDeny {
				t.Fatalf("read-only expected deny, got %#v", cls)
			}
			roReq.Approved = true
			if _, err := ts.ExecuteTool(roReq); err == nil {
				t.Fatalf("read-only ExecuteTool must deny even with Approved=true")
			}

			// In workspace-write mode, dangling symlinks are hard-denied; valid outside paths require one-time approval (ask) and fail when Approved=false.
			cls := ts.ClassifyToolCall(tc.req)
			if tc.name == "write through dangling symlink" {
				if cls.Decision != DecisionDeny {
					t.Fatalf("dangling symlink expected deny, got %#v", cls)
				}
			} else if cls.Decision != DecisionAsk {
				t.Fatalf("workspace-write out-of-workspace expected ask, got %#v", cls)
			}
			if _, err := ts.ExecuteTool(tc.req); err == nil {
				t.Fatalf("expected ExecuteTool to block unapproved out-of-workspace call")
			}
		})
	}

	// Ensure outside secret file is untouched.
	data, err := os.ReadFile(secretFile)
	if err != nil || string(data) != "top-secret" {
		t.Fatalf("outside secret file was modified or deleted: err=%v data=%q", err, string(data))
	}
}

func TestDeleteFileLstatTrashSafety(t *testing.T) {
	ts := &ToolService{}
	wsDir, outsideDir := setupTestWorkspace(t)

	// 1. Delete regular file -> moved into .vela-trash/
	regFile := filepath.Join(wsDir, "temp.txt")
	if err := os.WriteFile(regFile, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "delete_file",
		Workdir:        wsDir,
		PermissionMode: PermissionWorkspaceWrite,
		Input:          map[string]any{"file_path": "temp.txt"},
	})
	if err != nil || res.IsError {
		t.Fatalf("delete regular file failed: err=%v res=%#v", err, res)
	}
	if _, statErr := os.Lstat(regFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp.txt to be removed from original path")
	}
	trashed, _ := res.UIMeta["trashedPath"].(string)
	if trashed == "" || !strings.Contains(trashed, trashDirName) {
		t.Fatalf("expected trashedPath in .vela-trash, got %q", trashed)
	}
	if _, statErr := os.Lstat(trashed); statErr != nil {
		t.Fatalf("trashed file missing at %q: %v", trashed, statErr)
	}

	// 2. Delete a symlink inside workspace pointing to an outside file -> removes symlink only, outside file intact!
	outsideFile := filepath.Join(outsideDir, "keep-me.txt")
	if err := os.WriteFile(outsideFile, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	symInWS := filepath.Join(wsDir, "link_to_keep.txt")
	if err := os.Symlink(outsideFile, symInWS); err != nil {
		t.Fatal(err)
	}

	res2, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "delete_file",
		Workdir:        wsDir,
		PermissionMode: PermissionWorkspaceWrite,
		Input:          map[string]any{"file_path": "link_to_keep.txt"},
	})
	if err != nil || res2.IsError {
		t.Fatalf("delete symlink failed: err=%v res=%#v", err, res2)
	}
	if _, statErr := os.Lstat(symInWS); !os.IsNotExist(statErr) {
		t.Fatalf("expected symlink to be moved away")
	}
	gotOutside, err := os.ReadFile(outsideFile)
	if err != nil || string(gotOutside) != "precious" {
		t.Fatalf("outside target of symlink must remain intact! err=%v data=%q", err, string(gotOutside))
	}

	// 3. Refusing to delete workspace root itself (hard-denied at policy layer).
	_, err = ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "delete_file",
		Workdir:        wsDir,
		PermissionMode: PermissionWorkspaceWrite,
		Approved:       true,
		Input:          map[string]any{"file_path": "."},
	})
	if err == nil {
		t.Fatalf("expected policy deny when deleting workspace root")
	}
}

func TestPermissionHardBlocks(t *testing.T) {
	ts := &ToolService{}
	wsDir, outsideDir := setupTestWorkspace(t)
	outFile := filepath.Join(outsideDir, "ext.txt")
	if err := os.WriteFile(outFile, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}

	// read-only denies write/edit/delete_file even with Approved=true
	for _, tool := range []string{"write", "edit", "delete_file"} {
		req := ToolExecuteRequest{
			SessionID:      "s1",
			ToolName:       tool,
			Workdir:        wsDir,
			PermissionMode: PermissionReadOnly,
			Approved:       true,
			Input:          map[string]any{"file_path": "a.txt", "content": "x", "old_string": "a", "new_string": "b"},
		}
		if cls := ts.ClassifyToolCall(req); cls.Decision != DecisionDeny {
			t.Fatalf("read-only must deny %s, got %#v", tool, cls)
		}
		if _, err := ts.ExecuteTool(req); err == nil {
			t.Fatalf("ExecuteTool must deny %s in read-only mode", tool)
		}
	}

	// workspace-write requires one-time approval (ask) for outside read and allows once Approved=true
	wsOutReq := ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "read",
		Workdir:        wsDir,
		PermissionMode: PermissionWorkspaceWrite,
		Input:          map[string]any{"file_path": outFile},
	}
	if cls := ts.ClassifyToolCall(wsOutReq); cls.Decision != DecisionAsk {
		t.Fatalf("workspace-write must ask for outside read, got %#v", cls)
	}
	if _, err := ts.ExecuteTool(wsOutReq); err == nil {
		t.Fatalf("workspace-write outside read must fail when Approved=false")
	}
	wsOutApproved := wsOutReq
	wsOutApproved.Approved = true
	if res, err := ts.ExecuteTool(wsOutApproved); err != nil || res.IsError || !strings.Contains(res.ModelContent, "external") {
		t.Fatalf("workspace-write outside read should succeed once Approved=true: err=%v res=%#v", err, res)
	}

	// full-access allows outside read
	fullReq := wsOutReq
	fullReq.PermissionMode = PermissionFullAccess
	res, err := ts.ExecuteTool(fullReq)
	if err != nil || res.IsError || !strings.Contains(res.ModelContent, "external") {
		t.Fatalf("full-access outside read failed: err=%v res=%#v", err, res)
	}
}

func TestLazyScratchCreationAndRemoval(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	ts := &ToolService{}

	// Invalid sessionID must be denied and not create anything.
	badReq := ToolExecuteRequest{
		SessionID:      "../escape",
		ToolName:       "write",
		Workdir:        "",
		PermissionMode: PermissionWorkspaceWrite,
		Input:          map[string]any{"file_path": "index.html", "content": "<h1>hi</h1>"},
	}
	if _, err := ts.ExecuteTool(badReq); err == nil {
		t.Fatalf("expected invalid sessionID to be denied")
	}

	// Absolute outside path with empty workdir must be denied AND must NOT create scratch dir.
	escapeReq := ToolExecuteRequest{
		SessionID:      "sess-no-create",
		ToolName:       "write",
		Workdir:        "",
		PermissionMode: PermissionWorkspaceWrite,
		Input:          map[string]any{"file_path": filepath.Join(fakeHome, "outside.txt"), "content": "no"},
	}
	if _, err := ts.ExecuteTool(escapeReq); err == nil {
		t.Fatalf("expected outside write with empty workdir to be denied")
	}
	noCreateDir := filepath.Join(fakeHome, ".vela", "scratch", "sess-no-create")
	if _, err := os.Stat(noCreateDir); !os.IsNotExist(err) {
		t.Fatalf("scratch dir must NOT be created when path validation fails")
	}

	// Valid write with empty workdir lazily creates ~/.vela/scratch/<sessionId>/ and returns AssignedScratch.
	okReq := ToolExecuteRequest{
		SessionID:      "sess_ok-42",
		ToolName:       "write",
		Workdir:        "",
		PermissionMode: PermissionWorkspaceWrite,
		Input:          map[string]any{"file_path": "demo/game.html", "content": "<h1>game</h1>"},
	}
	res, err := ts.ExecuteTool(okReq)
	if err != nil || res.IsError {
		t.Fatalf("lazy scratch write failed: err=%v res=%#v", err, res)
	}
	if res.AssignedScratch == "" {
		t.Fatalf("expected AssignedScratch to be populated")
	}
	writtenFile := filepath.Join(res.AssignedScratch, "demo", "game.html")
	data, err := os.ReadFile(writtenFile)
	if err != nil || string(data) != "<h1>game</h1>" {
		t.Fatalf("expected file in scratch dir, err=%v data=%q", err, string(data))
	}

	// RemoveScratchDir rejects traversal and removes valid scratch dir.
	wsSvc := WorkspaceService{}
	if err := wsSvc.RemoveScratchDir("../bad"); err == nil {
		t.Fatalf("expected RemoveScratchDir to reject traversal sessionID")
	}
	if err := wsSvc.RemoveScratchDir("sess_ok-42"); err != nil {
		t.Fatalf("RemoveScratchDir failed: %v", err)
	}
	if _, err := os.Stat(res.AssignedScratch); !os.IsNotExist(err) {
		t.Fatalf("expected scratch dir to be removed")
	}
}

func TestGlobAndGrep(t *testing.T) {
	ts := &ToolService{}
	wsDir, _ := setupTestWorkspace(t)

	// Populate files including skipped directories.
	files := map[string]string{
		"src/a.go":                    "package main\n// TODO: alpha\n",
		"src/sub/b.go":                "package sub\n// TODO: beta\n",
		"src/c.ts":                    "export const x = 'TODO: gamma'\n",
		".git/config":                 "TODO: ignored git\n",
		"node_modules/pkg/index.js":   "TODO: ignored node_modules\n",
		".vela-trash/20260101-old.go": "TODO: ignored trash\n",
	}
	for rel, content := range files {
		full := filepath.Join(wsDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Make src/sub/b.go newer than src/a.go to verify mtime descending order.
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Minute)
	_ = os.Chtimes(filepath.Join(wsDir, "src", "a.go"), older, older)
	_ = os.Chtimes(filepath.Join(wsDir, "src", "sub", "b.go"), newer, newer)

	globRes, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "glob",
		Workdir:        wsDir,
		PermissionMode: PermissionReadOnly,
		Input:          map[string]any{"pattern": "**/*.go"},
	})
	if err != nil || globRes.IsError {
		t.Fatalf("glob failed: err=%v res=%#v", err, globRes)
	}
	globLines := strings.Split(globRes.ModelContent, "\n")
	if len(globLines) != 2 || globLines[0] != "src/sub/b.go" || globLines[1] != "src/a.go" {
		t.Fatalf("unexpected glob results (expected newest first, skipping .vela-trash): %#v", globLines)
	}

	grepRes, err := ts.ExecuteTool(ToolExecuteRequest{
		SessionID:      "s1",
		ToolName:       "grep",
		Workdir:        wsDir,
		PermissionMode: PermissionReadOnly,
		Input: map[string]any{
			"pattern": "TODO:\\s+(alpha|beta|gamma|ignored)",
			"include": "*.go",
		},
	})
	if err != nil || grepRes.IsError {
		t.Fatalf("grep failed: err=%v res=%#v", err, grepRes)
	}
	if !strings.Contains(grepRes.ModelContent, "alpha") || !strings.Contains(grepRes.ModelContent, "beta") {
		t.Fatalf("expected alpha & beta in grep output:\n%s", grepRes.ModelContent)
	}
	if strings.Contains(grepRes.ModelContent, "gamma") || strings.Contains(grepRes.ModelContent, "ignored") {
		t.Fatalf("grep leaked non-matching include or skipped dirs:\n%s", grepRes.ModelContent)
	}
}
