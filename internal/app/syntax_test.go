package app

import (
	"github.com/egoist/mygo/ui/editor"
	"testing"
)

func TestCMakeSyntax(t *testing.T) {
	for _, name := range []string{"CMakeLists.txt", "cmake/FindFoo.cmake"} {
		if languageFor(name) != cmakeLanguage {
			t.Errorf("%s not recognized", name)
		}
	}
	if l := languageFor("main.cpp"); l == nil || l.Name != "C++" {
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
