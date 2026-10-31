package app

import (
	"path/filepath"
	"testing"
)

func TestCLIPathOpensFileOrFolder(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	h := newHarness(t, "")
	h.a.openCLIPath(filepath.Join(root, "main.go"))
	if h.a.root != root || h.a.activePath() != filepath.Join(root, "main.go") {
		t.Fatalf("file: root=%q active=%q", h.a.root, h.a.activePath())
	}
	h.a.closeWorkspace()
	h.a.openCLIPath(root)
	if h.a.root != root {
		t.Fatalf("folder: root=%q", h.a.root)
	}
}
