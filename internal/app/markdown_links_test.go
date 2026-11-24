package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"see/internal/config"
	"see/internal/lsp"
)

// markdownLinkHarness opens a Markdown document no language server
// claims, so its links come from the parser.
func markdownLinkHarness(t *testing.T, files map[string]string) (*harness, *doc) {
	t.Helper()
	root := writeTree(t, files)
	h := newHarness(t, root)
	d := h.open("README.md")
	return h, d
}

func TestMarkdownDocLinksFromBuffer(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md": "# T\n\nSee [the guide](docs/guide.md) and ![logo](x.png).\n",
	})
	h.tt.Frame()
	if len(d.docLinks) != 2 {
		t.Fatalf("docLinks: %+v", d.docLinks)
	}
	link, image := d.docLinks[0], d.docLinks[1]
	text := d.buf.String()
	if got := text[link.start:link.end]; got != "[the guide](docs/guide.md)" {
		t.Fatalf("link covers %q, want the whole link", got)
	}
	if link.target != "docs/guide.md" || image.target != "x.png" {
		t.Fatalf("targets %q %q", link.target, image.target)
	}
	if d.linkBufVer != d.buf.Version() {
		t.Fatal("links not of the buffer's version")
	}
}

func TestMarkdownDocLinksRefreshOnEdit(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md": "[a](a.md)\n",
	})
	h.tt.Frame()
	before := len(d.docLinks)
	d.buf.Edit(editor.Edit{Text: "[b](b.md) "})
	h.tt.Frame()
	if len(d.docLinks) != before+1 || d.linkBufVer != d.buf.Version() {
		t.Fatalf("after edit: %+v ver %d", d.docLinks, d.linkBufVer)
	}
}

func TestMarkdownLinkClickOpensFile(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md":       "See [the guide](docs/guide.md).\n",
		"docs/guide.md":   "# Guide\n",
		"docs/my file.md": "x\n",
	})
	h.tt.Frame()
	off := d.docLinks[0].start + 1 // inside [the guide](docs/guide.md)
	h.a.goToDefinition(h.a.focus, d, off)
	h.settle(func() bool {
		return h.a.docs[filepath.Join(h.a.root, "docs", "guide.md")] != nil
	})
	if countCalls(h.fc.calls(), "documentLink") != 0 || countCalls(h.fc.calls(), "definition") != 0 {
		t.Fatalf("a Markdown link asked a server: %v", h.fc.calls())
	}
}

func TestMarkdownLinkClickOpensURL(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md": "See [the site](https://example.com/x).\n",
	})
	h.tt.Frame()
	h.a.goToDefinition(h.a.focus, d, d.docLinks[0].start+1)
	h.tt.Frame()
	if got := h.tt.OpenedURLs(); !slices.Equal(got, []string{"https://example.com/x"}) {
		t.Fatalf("opened %q", got)
	}
}

func TestMarkdownLinkClickOffLink(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md": "No links here.\n",
	})
	h.tt.Frame()
	h.a.goToDefinition(h.a.focus, d, 0)
	h.tt.Frame()
	if got := h.tt.OpenedURLs(); len(got) != 0 {
		t.Fatalf("a click off a link opened %q", got)
	}
}

func TestMarkdownLinkLineAnchor(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md": "[x](other.md#L3)\n",
		"other.md":  "one\ntwo\nthree\n",
	})
	h.tt.Frame()
	h.a.openDocLinkTarget(d, &d.docLinks[0])
	target := filepath.Join(h.a.root, "other.md")
	h.settle(func() bool { return h.a.focus.eds[target] != nil })
	if got := h.a.focus.eds[target].CursorPos(); got.Line != 2 {
		t.Fatalf("cursor %v, want line 2 of other.md", got)
	}
}

func TestMarkdownLinkHeadingAnchor(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md": "[x](other.md#getting-started)\n",
		"other.md":  "# Title\n\ntext\n\n## Getting Started\n\nbody\n",
	})
	h.tt.Frame()
	h.a.openDocLinkTarget(d, &d.docLinks[0])
	target := filepath.Join(h.a.root, "other.md")
	h.settle(func() bool { return h.a.focus.eds[target] != nil })
	if got := h.a.focus.eds[target].CursorPos(); got.Line != 4 {
		t.Fatalf("cursor %v, want the heading's line 4", got)
	}
}

func TestMarkdownLinkHeadingAnchorMissing(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md": "[x](other.md#nope)\n",
		"other.md":  "# Title\n",
	})
	h.tt.Frame()
	h.a.openDocLinkTarget(d, &d.docLinks[0])
	target := filepath.Join(h.a.root, "other.md")
	h.settle(func() bool { return h.a.docs[target] != nil })
	// No such heading lands at the top: the file still opened.
	if _, gone := h.a.focus.pending[target]; gone {
		t.Fatal("a pending position was left for an anchor that matched nothing")
	}
}

func TestMarkdownLinkSameDocAnchor(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md": "# Top\n\n[a](#sec)\n\ntext\n\n## Sec\n",
	})
	h.tt.Frame()
	// [a](#sec) jumps inside the buffer itself, opening nothing new.
	h.a.openDocLinkTarget(d, &d.docLinks[0])
	h.tt.Frame()
	if got := h.a.focus.eds[d.path].CursorPos(); got.Line != 6 {
		t.Fatalf("cursor %v, want the heading's line 6", got)
	}
	if len(h.a.docs) != 1 {
		t.Fatalf("a same-document anchor opened %+v", h.a.docs)
	}
}

func TestMarkdownPreviewOnLink(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{
		"README.md":     "[g](docs/guide.md) and [w](https://example.com).\n",
		"docs/guide.md": "# Guide\n",
	})
	o := markdownOptions(filepath.Dir(d.path))
	o.OnLink = func(dest string) { h.a.openMarkdownLink(d, o, dest) }

	o.OnLink("docs/guide.md")
	h.settle(func() bool {
		return h.a.docs[filepath.Join(h.a.root, "docs", "guide.md")] != nil
	})

	o.OnLink("https://example.com")
	h.tt.Frame()
	if got := h.tt.OpenedURLs(); !slices.Equal(got, []string{"https://example.com"}) {
		t.Fatalf("opened %q", got)
	}

	// A same-page anchor scrolls the view it came from, never a file.
	o.OnLink("#local")
	if o.Anchor != "#local" || o.AnchorVersion != 1 {
		t.Fatalf("anchor %+v", o)
	}
	o.OnLink("#local")
	if o.AnchorVersion != 2 {
		t.Fatal("asking the same anchor again did not bump AnchorVersion")
	}
}

func TestMarkdownPreviewOnLinkRejectsUnsafe(t *testing.T) {
	h, d := markdownLinkHarness(t, map[string]string{"README.md": "x\n"})
	o := &markdown.Options{}
	for _, dest := range []string{"javascript:alert(1)", "a\\b.md"} {
		before := len(h.a.docs)
		h.a.openMarkdownLink(d, o, dest)
		if len(h.a.docs) != before {
			t.Fatalf("%q opened a document", dest)
		}
	}
	if got := h.tt.OpenedURLs(); len(got) != 0 {
		t.Fatalf("unsafe destinations opened %q", got)
	}

	// A file URI of another host goes to the system, as a remote file.
	h.a.openMarkdownLink(d, o, "file://host/share/x")
	if got := h.tt.OpenedURLs(); !slices.Equal(got, []string{"file://host/share/x"}) {
		t.Fatalf("opened %q", got)
	}
}

func TestMarkdownLinkServerStaysDominant(t *testing.T) {
	// A Markdown file a configured server claims asks the server, as a
	// Go file does; the local parser fills only unserved documents.
	root := writeTree(t, map[string]string{"README.md": "[a](a.md)\n", "a.md": "x\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	// Simulate a server that claims .md files.
	h.a.customLSPSpecs = append(h.a.customLSPSpecs, config.LSPConfig{})
	i := len(h.a.customLSPSpecs) - 1
	h.a.customLSPs[i] = serverOf(root, h.fc)
	h.a.customCovers[i] = func(path string) bool { return isMarkdown(path) }
	h.fc.documentLinks = func(context.Context, string) ([]lsp.DocumentLink, error) {
		return []lsp.DocumentLink{}, nil
	}
	d := h.open("README.md")
	h.settle(func() bool {
		h.tt.Frame()
		return countCalls(h.fc.calls(), "documentLink") > 0
	})
	if len(d.docLinks) != 0 {
		t.Fatalf("a served document kept parser links: %+v", d.docLinks)
	}
}
