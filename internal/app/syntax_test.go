package app

import (
	"testing"

	"see/internal/workspace"

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
		{"notes", "echo '#!/bin/bash'\n", ""},
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
