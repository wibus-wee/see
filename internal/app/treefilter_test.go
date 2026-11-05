package app

import (
	"path/filepath"
	"testing"

	"see/internal/config"
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

func TestFilterHidesConfiguredNames(t *testing.T) {
	root := t.TempDir()
	a := &App{root: root, cfg: config.Load(""), quick: quickOpen{files: []string{".DS_Store", "docs/.DS_Store", "docs/visible.txt"}}}
	a.filter.query = ".DS_Store"
	a.syncFilter()
	if a.filter.matches != 0 {
		t.Fatalf("hidden files appeared in filter: %v", a.filter.roots)
	}
	a.cfg.HiddenNames = "docs"
	a.filter.forQuery = ""
	a.filter.query = "visible"
	a.syncFilter()
	if a.filter.matches != 0 || !a.hiddenPath(filepath.Join(root, "docs", "visible.txt")) {
		t.Fatal("configured folder did not hide its contents")
	}
	a.cfg.HiddenNames = ""
	a.filter.forQuery = ""
	a.syncFilter()
	if a.filter.matches != 1 {
		t.Fatal("clearing hidden names did not restore the file")
	}
}
