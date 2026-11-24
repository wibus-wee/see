package app

import (
	"testing"

	"see/internal/config"
	"see/internal/workspace"
	"see/locales"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestCMakeSyntax(t *testing.T) {
	for _, name := range []string{"CMakeLists.txt", "cmake/FindFoo.cmake"} {
		if languageFor(name, "") != cmakeLanguage {
			t.Errorf("%s not recognized", name)
		}
	}
	if l := languageFor("main.cpp", ""); l == nil || l.Name != "C++" {
		t.Fatalf("C++ language = %v", l)
	}
	tokens, state := cmakeTokenize("#[=[ comment", 0, nil)
	if state == 0 || tokens[len(tokens)-1].Kind != editor.TokenComment {
		t.Fatalf("comment: %v %d", tokens, state)
	}
	tokens, state = cmakeTokenize("end ]=] message(${FOO})", state, nil)
	if state != 0 {
		t.Fatalf("comment did not close: %d", state)
	}
	foundFunction, foundVariable := false, false
	for _, tok := range tokens {
		foundFunction = foundFunction || tok.Kind == editor.TokenFunction
		foundVariable = foundVariable || tok.Kind == editor.TokenVariable
	}
	if !foundFunction || !foundVariable {
		t.Fatalf("missing function or variable: %v", tokens)
	}
}

func TestScriptLanguageDetection(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"bin/tool", "#!/usr/bin/env -S python3 -u\nprint('hello')\n", "Python"},
		{"bin/tool", "#!/bin/bash -e\necho hello\n", "Shell"},
		{"tool.py", "#!/bin/bash\n", "Python"},
		{"CMakeLists.txt", "#!/bin/bash\n", "CMake"},
		{"notes", "echo '#!/bin/bash'\n", "Shell"},
	} {
		got := ""
		if lang := languageFor(tc.name, tc.text); lang != nil {
			got = lang.Name
		}
		if got != tc.want {
			t.Errorf("languageFor(%q, %q) = %q, want %q", tc.name, tc.text, got, tc.want)
		}
	}
}

func TestContentLanguageDetection(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"Untitled", "package main\n\nfunc main() {}\n", "Go"},
		{"Untitled", "let mut count = 1;", "Rust"},
		{"Untitled", "cmake_minimum_required(VERSION 3.20)\nproject(example)\n", "CMake"},
		{"tool", "def greet():\n    print('hello')\n", "Python"},
		{"config", "{\"name\": \"see\", \"enabled\": true}", "JSON"},
		{"config", "name: see\nversion: 1\n", "YAML"},
		{"Untitled", "# Notes\n\nA short description.\n", "Markdown"},
		{"notes", "This is ordinary prose.\nNo language markers here.\n", ""},
		{"main.go", "plain text", "Go"},
	} {
		got := ""
		if lang := languageFor(tc.name, tc.text); lang != nil {
			got = lang.Name
		}
		if got != tc.want {
			t.Errorf("languageFor(%q, %q) = %q, want %q", tc.name, tc.text, got, tc.want)
		}
	}
}

func TestUntitledDocumentLanguageCanBeDetectedAndOverridden(t *testing.T) {
	a := &App{}
	d := &doc{name: "Untitled", untitled: true, buf: editor.NewBuffer("")}
	a.detectDocumentLanguage(d)
	d.buf.Set("package main\nfunc main() {}\n")
	a.detectDocumentLanguage(d)
	if lang := d.buf.Language(); lang == nil || lang.Name != "Go" {
		t.Fatalf("detected language = %v, want Go", lang)
	}

	python := editor.LanguageNamed("Python")
	a.selectDocumentLanguage(d, python)
	d.buf.Set("echo hello\n")
	a.detectDocumentLanguage(d)
	if lang := d.buf.Language(); lang != python {
		t.Fatalf("manual language = %v, want Python", lang)
	}

	a.selectDocumentLanguage(d, nil)
	d.buf.Set("package main\n")
	a.detectDocumentLanguage(d)
	if lang := d.buf.Language(); lang != nil {
		t.Fatalf("manual plain-text mode = %v, want nil", lang)
	}
}

func TestDocumentLanguageMenuSwitchesUntitledDocument(t *testing.T) {
	a := &App{cfg: &config.Config{SoftWrap: true}}
	d := &doc{name: "Untitled", untitled: true, buf: editor.NewBuffer("")}
	st := &editor.State{Buffer: d.buf}
	a.layout = &layout{g: &group{eds: map[string]*editor.State{d.name: st}}}
	tt := ui.NewTester(func(c *ui.Context) { a.documentLanguageMenu(c, d) }, 500, 100)
	label := locales.Language() + ": " + locales.InspectorTypeText()
	st.SoftWrap = false
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Markdown"); err != nil {
		t.Fatal(err)
	}
	if lang := d.buf.Language(); lang == nil || lang.Name != "Markdown" {
		t.Fatalf("selected language = %v, want Markdown", lang)
	}
	if !st.SoftWrap {
		t.Fatal("language selection did not refresh editor preferences")
	}
	st.SoftWrap = false
	if err := tt.Click(locales.Language() + ": Markdown"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(locales.InspectorTypeText()); err != nil {
		t.Fatal(err)
	}
	d.buf.Set("package main\n")
	a.detectDocumentLanguage(d)
	if lang := d.buf.Language(); lang != nil {
		t.Fatalf("selected plain-text language changed to %q", lang.Name)
	}
	if !st.SoftWrap {
		t.Fatal("plain-text selection did not refresh editor preferences")
	}
}

func TestDocumentLanguageMenuTopRowIsClickableWhenAnchoredAtBottom(t *testing.T) {
	a := &App{cfg: &config.Config{SoftWrap: true}}
	d := &doc{name: "Untitled", untitled: true, buf: editor.NewBuffer("")}
	st := &editor.State{Buffer: d.buf}
	a.layout = &layout{g: &group{eds: map[string]*editor.State{d.name: st}}}
	a.selectDocumentLanguage(d, editor.LanguageNamed("Markdown"))

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Box(c).Grow(1)
			ui.Row(c).FillWidth().Height(24).Children(func() {
				a.documentLanguageMenu(c, d)
			})
		})
	}, 500, 928)
	tt.SetScale(2)
	if err := tt.Click(locales.Language() + ": Markdown"); err != nil {
		t.Fatal(err)
	}
	plainText, ok := tt.Find(locales.InspectorTypeText())
	if !ok {
		t.Fatal("Plain Text menu item is not visible")
	}
	if plainText.Y < 32 {
		t.Fatalf("Plain Text menu item starts too close to the window edge: y=%v", plainText.Y)
	}
	if err := tt.Click(locales.InspectorTypeText()); err != nil {
		t.Fatalf("clicking Plain Text: %v", err)
	}
	if lang := d.buf.Language(); lang != nil {
		t.Fatalf("selected language = %q, want Plain Text", lang.Name)
	}

	if err := tt.Click(locales.Language() + ": " + locales.InspectorTypeText()); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	want := documentLanguages()[len(documentLanguages())-1]
	if lang := d.buf.Language(); lang != want {
		t.Fatalf("keyboard selection = %v, want %v", lang, want)
	}
}

func TestReloadScriptLanguage(t *testing.T) {
	a := &App{}
	d := &doc{path: "bin/tool", name: "tool", buf: editor.NewBuffer("#!/bin/bash\necho hello\n")}
	d.buf.SetLanguage(languageFor(d.name, d.buf.Line(0)))
	for _, tc := range []struct{ text, want string }{
		{"#!/usr/bin/env python3\nprint('hello')\n", "Python"},
		{"plain text\n", ""},
		{"#!/bin/bash\necho hello\n", "Shell"},
	} {
		a.loaded(d, workspace.File{Kind: workspace.Text, Text: tc.text}, nil, false, nil)
		got := ""
		if lang := d.buf.Language(); lang != nil {
			got = lang.Name
		}
		if got != tc.want {
			t.Fatalf("reloaded language = %q, want %q", got, tc.want)
		}
		if d.buf.Snapshot().String() != tc.text || d.buf.Modified() {
			t.Fatal("reload did not preserve clean disk text")
		}
	}
}
