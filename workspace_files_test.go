package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchWorkspaceFiles(t *testing.T) {
	root := t.TempDir()
	mustMkdir := func(rel string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
	}
	mustWrite := func(rel string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir parent %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	mustWrite("README.md")
	mustWrite("go.mod")
	mustWrite("src/services/chat/db.ts")
	mustWrite("src/services/chat/history/README.md")
	mustWrite("src/components/chatInput/ChatInput.tsx")
	mustWrite("node_modules/pkg/chat.js")
	mustMkdir(".git/objects")

	svc := WorkspaceService{}

	// 1. Empty query lists root directory (dirs first, hidden & excluded omitted)
	rootItems, err := svc.SearchWorkspaceFiles(root, "", 20)
	if err != nil {
		t.Fatalf("root search failed: %v", err)
	}
	if len(rootItems) != 3 || rootItems[0].Path != "src" || rootItems[0].Kind != "directory" {
		t.Fatalf("unexpected root items: %+v", rootItems)
	}

	// 2. Fuzzy query "chat" ranks directories first and excludes node_modules
	chatItems, err := svc.SearchWorkspaceFiles(root, "chat", 20)
	if err != nil {
		t.Fatalf("chat search failed: %v", err)
	}
	if len(chatItems) == 0 || chatItems[0].Path != "src/services/chat" || chatItems[0].Kind != "directory" {
		t.Fatalf("expected src/services/chat first, got: %+v", chatItems)
	}
	for _, item := range chatItems {
		if item.Path == "node_modules/pkg/chat.js" {
			t.Fatalf("node_modules entry should be excluded: %+v", item)
		}
	}

	// 3. Duplicate filename "readme.md" returns both root and nested paths
	readmeItems, err := svc.SearchWorkspaceFiles(root, "readme.md", 20)
	if err != nil {
		t.Fatalf("readme search failed: %v", err)
	}
	if len(readmeItems) != 2 || readmeItems[0].Path != "README.md" || readmeItems[1].Path != "src/services/chat/history/README.md" {
		t.Fatalf("unexpected readme items: %+v", readmeItems)
	}

	// 4. Directory drill-down lists direct children of src/services/chat/
	drilled, err := svc.SearchWorkspaceFiles(root, "src/services/chat/", 20)
	if err != nil {
		t.Fatalf("drilled search failed: %v", err)
	}
	if len(drilled) != 2 || drilled[0].Path != "src/services/chat/history" || drilled[1].Path != "src/services/chat/db.ts" {
		t.Fatalf("unexpected drilled items: %+v", drilled)
	}

	// 5. Path traversal outside workspace returns empty
	escaped, err := svc.SearchWorkspaceFiles(root, "../", 20)
	if err != nil {
		t.Fatalf("escaped search failed: %v", err)
	}
	if len(escaped) != 0 {
		t.Fatalf("expected empty result for traversal query, got: %+v", escaped)
	}
}
