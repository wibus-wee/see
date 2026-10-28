package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"see/internal/lsp"
)

type languageRecordingClient struct {
	*fakeClient
	language chan string
}

func (c *languageRecordingClient) DidOpen(uri, language string, version int, text string) error {
	c.language <- language
	return c.fakeClient.DidOpen(uri, language, version, text)
}

func TestTypeScriptDefinitionRoutesToOwnServer(t *testing.T) {
	root := writeTree(t, map[string]string{"a.tsx": "const App = thing;", "b.ts": "export const thing = 1;"})
	h := newHarness(t, root)
	h.a.cfg.trust(root)
	fc := &languageRecordingClient{fakeClient: &fakeClient{enc: lsp.UTF16}, language: make(chan string, 1)}
	target := filepath.Join(root, "b.ts")
	fc.def = func(context.Context, string, lsp.Position) ([]lsp.Location, error) {
		return []lsp.Location{{URI: uriOf(target)}}, nil
	}
	h.a.tsLSP.stop()
	h.a.tsLSP = newGoLSP(root, func(context.Context, string, func(error)) (langClient, error) { return fc, nil }, nil)
	h.a.tsLSP.name = "typescript-language-server"
	h.a.tsLSP.language = typeScriptLanguage
	defer h.a.tsLSP.stopWait(time.Second)
	d := h.open("a.tsx")
	h.a.goToDefinition(h.a.focus, d, at(d, "thing"))
	h.settle(func() bool { return h.a.activePath() == target })
	select {
	case got := <-fc.language:
		if got != "typescriptreact" {
			t.Fatalf("language ID = %q", got)
		}
	default:
		t.Fatal("document was not opened")
	}
	if len(h.fc.calls()) != 0 {
		t.Fatalf("Go server received calls: %v", h.fc.calls())
	}
}

func TestTypeScriptServerRejectsWorkspaceExecutable(t *testing.T) {
	root := t.TempDir()
	server := filepath.Join(root, "server")
	if err := os.WriteFile(server, []byte("server"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_TYPESCRIPT_LANGUAGE_SERVER", server)
	if _, err := findTypeScriptServer(root); err == nil {
		t.Fatal("accepted a workspace executable")
	}
	external := filepath.Join(t.TempDir(), "server-link")
	if err := os.Symlink(server, external); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_TYPESCRIPT_LANGUAGE_SERVER", external)
	if _, err := findTypeScriptServer(root); err == nil {
		t.Fatal("accepted a symlink into workspace")
	}
}
