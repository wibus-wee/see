package languages

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"see/internal/config"
)

func TestConfiguredLanguage(t *testing.T) {
	spec := config.LSPConfig{
		Extensions: []string{".rs", ".toml"},
		Languages:  []string{"rust"},
	}
	lang := ConfiguredLanguage(spec)
	if got := lang("src/main.rs"); got != "rust" {
		t.Fatalf("lang(.rs) = %q", got)
	}
	if got := lang("a.toml"); got != "rust" {
		t.Fatalf("lang(.toml) = %q, want the single language", got)
	}
	if got := lang("a.go"); got != "" {
		t.Fatalf("lang(.go) = %q, want none", got)
	}
	// One language per extension, as TypeScript's.
	multi := ConfiguredLanguage(config.LSPConfig{
		Extensions: []string{".ts", ".js"},
		Languages:  []string{"typescript", "javascript"},
	})
	if got := multi("a.js"); got != "javascript" {
		t.Fatalf("lang(.js) = %q", got)
	}
	// No dot in the config still matches the dotted extension, and a
	// spec without languages reports none.
	nodot := ConfiguredLanguage(config.LSPConfig{Extensions: []string{"rs"}})
	if got := nodot("a.rs"); got != "" {
		t.Fatalf("lang with no languages = %q, want none", got)
	}
	if !ConfiguredCovers(config.LSPConfig{Extensions: []string{"rs"}})("a.rs") {
		t.Fatal("an extension without its dot did not match")
	}
}

func TestConfiguredCovers(t *testing.T) {
	byExt := ConfiguredCovers(config.LSPConfig{Extensions: []string{".rs"}})
	if !byExt("a.rs") || byExt("a.go") || byExt("a.py") {
		t.Fatal("extension coverage wrong")
	}
	// Language-only coverage names files by their known language.
	byLang := ConfiguredCovers(config.LSPConfig{Languages: []string{"c", "cpp"}})
	if !byLang("a.c") || !byLang("a.cc") || byLang("a.rs") || byLang("a.go") {
		t.Fatal("language coverage wrong")
	}
}

func TestFindConfigured(t *testing.T) {
	root := t.TempDir()
	bin := t.TempDir() // outside the workspace
	fake := filepath.Join(bin, "myls")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindConfigured(root, config.LSPConfig{Name: "myls", Command: []string{fake, "--stdio"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != fake {
		t.Fatalf("FindConfigured = %q, want %q", got, fake)
	}
	// A server inside the workspace is never used.
	inside := filepath.Join(root, "myls")
	if runtime.GOOS == "windows" {
		inside += ".exe"
	}
	if err := os.WriteFile(inside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := FindConfigured(root, config.LSPConfig{Name: "myls", Command: []string{inside}}); err == nil {
		t.Fatal("a server inside the workspace was accepted")
	}
	// A relative name resolves on PATH; a missing one reports itself.
	if _, err := FindConfigured(root, config.LSPConfig{Name: "myls", Command: []string{"see-no-such-server-xyz"}}); err == nil {
		t.Fatal("a missing server resolved")
	}
}

func TestConfiguredSpecsDropsEmpty(t *testing.T) {
	got := ConfiguredSpecs([]config.LSPConfig{
		{Name: "ok", Extensions: []string{".rs"}, Command: []string{"myls"}},
		{Name: "", Extensions: []string{".rs"}, Command: []string{"myls"}},
		{Name: "no-command", Extensions: []string{".rs"}},
		{Name: "no-coverage", Command: []string{"myls"}},
	})
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("kept %+v", got)
	}
}
