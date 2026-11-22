package app

import (
	"see/internal/config"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestShowInvisiblesDrawsWhitespaceEverywhere(t *testing.T) {
	a := &App{cfg: &config.Config{}}
	st := &editor.State{}
	a.applyEditorPrefs(st)
	if st.Whitespace != editor.WhitespaceSelection {
		t.Fatalf("whitespace = %v by default, want selection only", st.Whitespace)
	}
	a.cfg.ShowInvisibles = true
	a.applyEditorPrefs(st)
	if st.Whitespace != editor.WhitespaceAll {
		t.Fatalf("whitespace = %v with Show Invisibles, want all", st.Whitespace)
	}
	a.cfg.ShowInvisibles = false
	a.applyEditorPrefs(st)
	if st.Whitespace != editor.WhitespaceSelection {
		t.Fatalf("whitespace = %v after turning Show Invisibles off, want selection only", st.Whitespace)
	}
}
