package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/internal/config"
)

func TestSavedFindQueryRerunsWithOptionsAfterReopen(t *testing.T) {
	if _, err := findRipgrep(""); err != nil {
		t.Skip(err)
	}
	root := writeTree(t, map[string]string{
		"notes.txt":      "TODO-1\ntodo-2\nNOTTODO-3\n",
		"notes_test.txt": "TODO-4\n",
		"notes.md":       "TODO-5\n",
	})
	h := newHarness(t, root)
	configPath := filepath.Join(t.TempDir(), "config.json")
	h.a.cfg.SetPath(configPath)
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tt.Type("TODO-[0-9]+")
	for _, control := range []string{"Match Case", "Whole Words", "Regular Expression", "Filters"} {
		clickFindControl(t, h, control)
	}
	for _, filter := range []struct{ key, value string }{
		{"Include: *.go, cmd/**", "*.txt"},
		{"Exclude: *_test.go", "*_test.txt"},
	} {
		field, ok := h.tt.AccessByRole(ui.RoleTextField, filter.key)
		if !ok {
			t.Fatalf("missing filter %q", filter.key)
		}
		h.tt.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
		h.tt.Type(filter.value)
	}
	chooseSavedFindItem(t, h, "Save Current Query")
	h.a.closeWorkspace()

	h = newHarness(t, "")
	h.a.cfg = config.Load(configPath)
	h.a.openWorkspace(root)
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyH)
	h.tt.Type("unrelated")
	clickFindControl(t, h, "Replace with")
	h.tt.Type("must not be saved or applied")
	path := filepath.Join(h.a.root, "notes.txt")
	if err := os.WriteFile(path, []byte("TODO-6\nTODO-7\ntodo-8\nNOTTODO-9\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	chooseSavedFindItem(t, h, "TODO-[0-9]+")
	h.settle(func() bool { return !h.a.find.running && h.a.find.stats.Matches == 2 })
	if _, ok := h.tt.Find("2 results in 1 files"); !ok {
		t.Fatalf("missing refreshed result summary: %v", h.tt.Texts())
	}
	if len(h.a.find.results) != 1 || h.a.find.results[0].Path != path {
		t.Fatalf("results did not respect the saved file filters: %+v", h.a.find.results)
	}
	if _, ok := h.tt.Find("Replace with"); ok {
		t.Fatal("rerunning a query left the replacement controls visible")
	}
	if got := readFindTestFile(t, path); got != "TODO-6\nTODO-7\ntodo-8\nNOTTODO-9\n" {
		t.Fatalf("rerunning the search edited the file: %q", got)
	}

	other := writeTree(t, map[string]string{"other.txt": "TODO-0\n"})
	h.a.openWorkspace(other)
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	clickFindControl(t, h, "Saved Find Queries")
	if got := h.tt.Menu(); slices.Contains(got, "TODO-[0-9]+") {
		t.Fatalf("another workspace inherited the saved query: %v", got)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.a.openWorkspace(root)
	h.tt.Frame()
	chooseSavedFindItem(t, h, "Remove Current Query")
	h.a.closeWorkspace()
	if saved := config.Load(configPath).Workspaces[root].SavedFindQueries; len(saved) != 0 {
		t.Fatalf("removed query was still persisted: %+v", saved)
	}
}

func chooseSavedFindItem(t *testing.T, h *harness, label string) {
	t.Helper()
	clickFindControl(t, h, "Saved Find Queries")
	if err := h.tt.ChooseMenuItem(label); err != nil {
		t.Fatalf("%v; menu = %v", err, h.tt.Menu())
	}
}
