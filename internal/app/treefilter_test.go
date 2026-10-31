package app

import (
	"path/filepath"
	"testing"
)

func TestFilterSavesOpenFolders(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep")
	docs := filepath.Join(root, "docs")
	a := &App{root: root, quick: quickOpen{files: []string{"docs/Guide.md", "keep/a.go"}}}
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
