package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestInlineDiagnostics(t *testing.T) {
	h := newHarness(t, gitRepo(t, map[string]string{"a.txt": "one\ntwo\nthree\nfour\n"}))
	h.readGit()
	d := h.open("a.txt")
	h.settle(func() bool { return d.blameReady })
	st := h.a.activeEditor()
	d.languageInlays = []editor.Inlay{{Offset: 0, Text: "type hint"}}
	d.diagnosticVersion = d.buf.Version()
	d.diagnostics = []editor.Diagnostic{
		{Start: 1, Severity: editor.SeverityWarning, Message: "warning"},
		{Start: 0, Severity: editor.SeverityError, Message: "  bad\t value  \r\nmore detail"},
		{Start: 2, Severity: editor.SeverityInfo, Message: "info"},
		{Start: 0, Severity: editor.SeverityHint, Message: "ignored"},
		{Start: 4, Severity: editor.SeverityWarning, Message: "warning only"},
		{Start: 8, Severity: editor.SeverityInfo, Message: "info only"},
		{Start: 14, Severity: editor.SeverityHint, Message: "hint only"},
		{Start: -1, Message: "invalid"},
		{Start: 100, Message: "invalid"},
	}
	inlays := h.a.editorInlays(d, st)
	if len(inlays) != 4 || inlays[0].Text != "type hint" {
		t.Fatalf("inlays: %+v", inlays)
	}
	for i, want := range []struct {
		text   string
		offset int
	}{{"    bad value (+2)", 3}, {"    warning only", 7}, {"    info only", 13}} {
		got := inlays[i+1]
		if got.Text != want.text || got.Offset != want.offset || !got.Trailing || got.Ellipsis != "…" {
			t.Fatalf("diagnostic %d: %+v", i, got)
		}
	}
	base := editor.LineBackground{First: 3, Last: 3, Color: ui.RGB(1, 2, 3)}
	st.LineBackgrounds = []editor.LineBackground{base}
	backgrounds := h.a.diagnosticLineBackgrounds(d, st)
	if len(backgrounds) != 4 || backgrounds[0] != base || backgrounds[1].Color != st.Theme.Error.Alpha(0.10) || backgrounds[2].Color != st.Theme.Warning.Alpha(0.10) || backgrounds[3].Color != st.Theme.Info.Alpha(0.10) {
		t.Fatalf("backgrounds: %+v", backgrounds)
	}
	h.tt.Frame()
	if len(st.LineBackgrounds) != 1 || st.LineBackgrounds[0] != base {
		t.Fatalf("base backgrounds changed: %+v", st.LineBackgrounds)
	}
	st.Select(14, 14)
	if got := h.a.editorInlays(d, st); len(got) != 5 {
		t.Fatalf("clean line missing blame: %+v", got)
	}
	st.Select(0, 0)
	for _, mode := range []string{"hidden", "stale", "empty"} {
		h.a.cfg.HideInlineDiagnostics = mode == "hidden"
		d.diagnosticVersion = d.buf.Version()
		if mode == "stale" {
			d.diagnosticVersion++
		}
		if mode == "empty" {
			d.diagnostics = nil
		}
		if got := h.a.editorInlays(d, st); len(got) != 2 || got[0].Text != "type hint" {
			t.Fatalf("%s inlays: %+v", mode, got)
		}
		if got := h.a.diagnosticLineBackgrounds(d, st); len(got) != 1 || got[0] != base {
			t.Fatalf("%s backgrounds: %+v", mode, got)
		}
	}
}
