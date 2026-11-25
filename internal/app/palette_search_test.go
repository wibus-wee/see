package app

import (
	"testing"

	"github.com/egoist/mygo/i18n"
)

func rankedCommandIDs(t *testing.T, h *harness, query string) []string {
	t.Helper()
	h.a.openPalette()
	h.a.quick.query = ">" + query
	h.a.rankQuick()
	ids := make([]string, len(h.a.quick.commands))
	for i, cmd := range h.a.quick.commands {
		ids[i] = cmd.id
	}
	return ids
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestPaletteMatchesDottedID(t *testing.T) {
	h := newHarness(t, "")
	ids := rankedCommandIDs(t, h, "font.big")
	if len(ids) == 0 || ids[0] != "font.bigger" {
		t.Fatalf("font.big should rank font.bigger first: %v", ids[:min(5, len(ids))])
	}
}

func TestPaletteMatchesEnglishName(t *testing.T) {
	h := newHarness(t, "")
	ids := rankedCommandIDs(t, h, "Bigger")
	if !containsID(ids, "font.bigger") {
		t.Fatalf("Bigger did not match font.bigger")
	}
}

func TestPaletteMatchesLocalizedTitle(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	defer i18n.SetLocaleFunc(nil)
	h := newHarness(t, "")
	ids := rankedCommandIDs(t, h, "增大")
	if !containsID(ids, "font.bigger") {
		t.Fatalf("增大 did not match font.bigger")
	}
	ids = rankedCommandIDs(t, h, "增大字号")
	if len(ids) == 0 || ids[0] != "font.bigger" {
		t.Fatalf("exact localized query should rank first: %v", ids[:min(3, len(ids))])
	}
}

// The user's case: the UI is in Chinese, the command shows as 增大字号,
// and typing English should still find it.
func TestPaletteEnglishQueryFindsChineseOnlyCommand(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	defer i18n.SetLocaleFunc(nil)
	h := newHarness(t, "")
	ids := rankedCommandIDs(t, h, "bigger")
	if !containsID(ids, "font.bigger") {
		t.Fatalf("english query bigger did not match font.bigger under zh-Hans")
	}
	ids = rankedCommandIDs(t, h, "font.bigger")
	if len(ids) == 0 || ids[0] != "font.bigger" {
		t.Fatalf("exact id query should rank the command first: %v", ids[:min(3, len(ids))])
	}
}

func TestPaletteExactIDRanksFirst(t *testing.T) {
	h := newHarness(t, "")
	cases := []struct{ query, want string }{
		{"font.bigger", "font.bigger"},
		{"git.pull", "git.pull"},
		{"gitpush", "git.push"},
		{"foldall", "fold.all"},
		{"save", "file.save"},
	}
	for _, c := range cases {
		ids := rankedCommandIDs(t, h, c.query)
		if len(ids) == 0 {
			t.Errorf("%q: no results", c.query)
			continue
		}
		if ids[0] != c.want {
			t.Errorf("%q: top = %v, want %s", c.query, ids[:min(3, len(ids))], c.want)
		}
	}
}

func TestPaletteExecutesChosenCommand(t *testing.T) {
	h := newHarness(t, "")
	h.a.newFile()
	before := len(h.a.groups())
	h.a.openPalette()
	h.a.quick.query = ">editorGroup.splitRight"
	h.a.rankQuick()
	h.a.chooseQuick(0)
	if len(h.a.groups()) != before+1 {
		t.Fatal("choosing by id did not run the command")
	}
}
