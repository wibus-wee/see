package app

import (
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
)

func TestEditorAndDiffSoftWrapUseDocumentLanguage(t *testing.T) {
	a := &App{cfg: &config.Config{
		SoftWrap: true,
		SoftWrapByLanguage: map[string]bool{
			"Markdown": false,
			"Go":       true,
			"Python":   false,
		},
	}}

	buffers := []struct {
		name string
		want bool
	}{
		{name: "README.md", want: false},
		{name: "main.go", want: true},
		{name: "script.py", want: false},
		{name: "notes.unknown", want: true},
	}
	for _, tt := range buffers {
		buf := editor.NewBuffer("long line")
		if language := languageFor(tt.name, ""); language != nil {
			buf.SetLanguage(language)
		}
		st := &editor.State{Buffer: buf}
		a.applyEditorPrefs(st)
		if st.SoftWrap != tt.want {
			t.Errorf("editor %s SoftWrap = %v, want %v", tt.name, st.SoftWrap, tt.want)
		}
		diff := &editor.DiffState{New: buf}
		a.applyDiffPrefs(diff)
		if diff.Wrap != tt.want {
			t.Errorf("diff %s Wrap = %v, want %v", tt.name, diff.Wrap, tt.want)
		}
	}
}
