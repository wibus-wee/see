package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestBuildFilterTree(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"main.go",
		"internal/app/model.go",
		"internal/app/model_test.go",
		"docs/Guide.md",
		"node_modules/x/index.js",
	}
	tree, roots, matches := buildFilterTree(root, files, "model")
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
	// A folder comes before the files beside it.
	tree, roots, _ = buildFilterTree(root, files, "")
	if want := []string{"docs", "internal", "node_modules", "main.go"}; !slices.Equal(baseNames(roots), want) {
		t.Errorf("root entries = %v, want %v", baseNames(roots), want)
	}
	if !slices.Equal(baseNames(tree[root]), baseNames(roots)) {
		t.Errorf("root differs from roots: %v and %v", baseNames(tree[root]), baseNames(roots))
	}
	if _, _, matches := buildFilterTree(root, files, "zzz"); matches != 0 {
		t.Errorf("no match: %d", matches)
	}
}

func TestBuildFilterTreeWords(t *testing.T) {
	root := t.TempDir()
	files := []string{"internal/app/Model.go", "internal/lsp/client.go", "docs/model.md"}
	_, _, matches := buildFilterTree(root, files, "app model")
	if matches != 1 {
		t.Errorf("matches = %d, want 1", matches)
	}
	// Words match any case, in any order, along the path.
	if _, _, matches := buildFilterTree(root, files, "MODEL app"); matches != 1 {
		t.Errorf("case and order: matches = %d, want 1", matches)
	}
}

func TestFilterSavesOpenFolders(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep")
	docs := filepath.Join(root, "docs")
	a := &app{root: root, quick: quickOpen{files: []string{"docs/Guide.md", "keep/a.go"}}}
	a.tree.Open.Add(keep)

	a.filter.query = "guide"
	a.syncFilter()
	if !a.filtering() || a.filter.tree == nil {
		t.Fatalf("filter did not build: %+v", a.filter)
	}
	if !a.tree.Open.Has(docs) {
		t.Error("a matching folder did not open")
	}

	a.filter.query = ""
	a.syncFilter()
	if a.filtering() || a.filter.tree != nil {
		t.Errorf("filter did not clear: %+v", a.filter)
	}
	if !a.tree.Open.Has(keep) {
		t.Error("the folders open before filtering did not come back")
	}
	if a.tree.Open.Has(docs) {
		t.Error("a folder the filter opened stayed open")
	}
}

func baseNames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}
