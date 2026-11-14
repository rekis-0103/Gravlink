package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExplorerSecurityAndReadOnly(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("API_KEY=123"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "private.pem"), []byte("KEY"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exp, err := NewExplorer(root)
	if err != nil {
		t.Fatalf("NewExplorer failed: %v", err)
	}

	// Path traversal must fail
	if _, err := exp.ReadFile("../secret.txt"); !errors.Is(err, ErrPathTraversal) {
		t.Fatalf("expected ErrPathTraversal, got %v", err)
	}

	// Sensitive files must fail
	if _, err := exp.ReadFile(".env"); !errors.Is(err, ErrSensitiveFile) {
		t.Fatalf("expected ErrSensitiveFile for .env, got %v", err)
	}
	if _, err := exp.ReadFile("private.pem"); !errors.Is(err, ErrSensitiveFile) {
		t.Fatalf("expected ErrSensitiveFile for .pem, got %v", err)
	}

	// Valid file succeeds
	fc, err := exp.ReadFile("cmd/main.go")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if fc.Language != "go" || fc.Path != "cmd/main.go" {
		t.Fatalf("unexpected FileContent: %+v", fc)
	}

	// Tree omits sensitive files
	tree, err := exp.ListTree("", 3)
	if err != nil {
		t.Fatalf("ListTree failed: %v", err)
	}
	for _, n := range tree {
		if n.Name == ".env" || n.Name == "private.pem" {
			t.Fatalf("sensitive file leaked in tree: %s", n.Name)
		}
	}
}
