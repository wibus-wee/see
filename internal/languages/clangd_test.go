package languages

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeLanguages(t *testing.T) {
	for name, want := range map[string]string{"main.c": "c", "main.cpp": "cpp", "include/a.hpp": "cpp", "main.mm": "objective-cpp", "main.go": ""} {
		if got := ClangLanguage(name); got != want {
			t.Errorf("ClangLanguage(%q) = %q, want %q", name, got, want)
		}
	}
	for name, want := range map[string]string{"CMakeLists.txt": "cmake", "lib/FindFoo.cmake": "cmake", "cmakelists.txt": "", "main.cpp": ""} {
		if got := CMakeLanguage(name); got != want {
			t.Errorf("CMakeLanguage(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestServerDiscoveryRejectsWorkspaceExecutable(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "server")
	if err := os.WriteFile(exe, []byte("server"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		env  string
		find func(string) (string, error)
	}{{"SEE_CLANGD", FindClangd}, {"SEE_CMAKE_LANGUAGE_SERVER", FindCMakeServer}} {
		t.Setenv(tc.env, exe)
		if _, err := tc.find(root); err == nil {
			t.Errorf("%s accepted a workspace executable", tc.env)
		}
		if got, err := tc.find(t.TempDir()); err != nil || got != exe {
			t.Errorf("%s = %q, %v", tc.env, got, err)
		}
	}
}
