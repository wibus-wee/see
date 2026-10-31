package treefilter

import (
	"path/filepath"
	"slices"
	"testing"
)

func baseNames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

func TestBuildTreeFindsFilesInUnopenedFolders(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"main.go",
		"internal/app/model.go",
		"internal/app/model_test.go",
		"docs/Guide.md",
		"node_modules/x/index.js",
	}
	tree, roots, matches := BuildTree(root, files, "model")
	if matches != 2 {
		t.Errorf("matches = %d, want 2", matches)
	}
	if want := []string{"internal"}; !slices.Equal(baseNames(roots), want) {
		t.Errorf("roots = %v, want %v", baseNames(roots), want)
	}
	appDir := filepath.Join(root, "internal", "app")
	if got, want := baseNames(tree[appDir]), []string{"model.go", "model_test.go"}; !slices.Equal(got, want) {
		t.Errorf("app entries = %v, want %v", got, want)
	}
}

func TestBuildTreePutsFoldersFirst(t *testing.T) {
	root := t.TempDir()
	files := []string{"main.go", "internal/app/model.go", "docs/Guide.md", "node_modules/x/index.js"}
	tree, roots, _ := BuildTree(root, files, "")
	if want := []string{"docs", "internal", "node_modules", "main.go"}; !slices.Equal(baseNames(roots), want) {
		t.Errorf("root entries = %v, want %v", baseNames(roots), want)
	}
	if !slices.Equal(baseNames(tree[root]), baseNames(roots)) {
		t.Errorf("root differs from roots: %v and %v", baseNames(tree[root]), baseNames(roots))
	}
	if _, _, matches := BuildTree(root, files, "zzz"); matches != 0 {
		t.Errorf("no match: %d", matches)
	}
}

func TestBuildTreeMatchesEveryWordInAnyCase(t *testing.T) {
	root := t.TempDir()
	files := []string{"internal/app/Model.go", "internal/lsp/client.go", "docs/model.md"}
	if _, _, matches := BuildTree(root, files, "app model"); matches != 1 {
		t.Errorf("matches = %d, want 1", matches)
	}
	if _, _, matches := BuildTree(root, files, "MODEL app"); matches != 1 {
		t.Errorf("case and order: matches = %d, want 1", matches)
	}
}

func TestBuildTreeKeepsFilesInsideRoot(t *testing.T) {
	root := t.TempDir()
	files := []string{"../outside.go", "inside.go"}
	_, roots, matches := BuildTree(root, files, "")
	if matches != 1 {
		t.Errorf("matches = %d, want 1", matches)
	}
	if want := []string{"inside.go"}; !slices.Equal(baseNames(roots), want) {
		t.Errorf("roots = %v, want %v", baseNames(roots), want)
	}
}
