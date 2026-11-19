package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/uri"
)

// linkHarness opens a Go file whose fake server answers document links.
func linkHarness(t *testing.T, links func(context.Context, string) ([]lsp.DocumentLink, error)) (*harness, *doc) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"main.go":  "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }\n",
		"lib/x.go": "package lib\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.documentLinks = links
	d := h.open("main.go")
	return h, d
}

func settleLinks(h *harness, d *doc) {
	h.settle(func() bool {
		h.tt.Frame()
		return len(d.docLinks) > 0
	})
}

func TestDocumentLinksFillDocumentLane(t *testing.T) {
	var gotURI string
	h, d := linkHarness(t, func(_ context.Context, uri string) ([]lsp.DocumentLink, error) {
		gotURI = uri
		return []lsp.DocumentLink{
			{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 8}, End: lsp.Position{Line: 2, Character: 13}}, Target: "https://pkg.go.dev/fmt"},
		}, nil
	})
	settleLinks(h, d)
	if gotURI != d.uri {
		t.Fatalf("documentLink asked for %q, want the document's URI", gotURI)
	}
	snap := d.buf.Snapshot()
	wantStart := snap.LineStart(2) + lsp.ColumnFromLSP(snap.Line(2), 8, h.fc.enc)
	wantEnd := snap.LineStart(2) + lsp.ColumnFromLSP(snap.Line(2), 13, h.fc.enc)
	if len(d.docLinks) != 1 {
		t.Fatalf("docLinks: %+v", d.docLinks)
	}
	l := d.docLinks[0]
	if l.start != wantStart || l.end != wantEnd || l.target != "https://pkg.go.dev/fmt" {
		t.Fatalf("link = %+v, want %d..%d", l, wantStart, wantEnd)
	}
}

func TestDocumentLinksUnservedAsksNothing(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	h := newHarness(t, root) // no trust granted: the server must not be asked
	h.fc.documentLinks = func(context.Context, string) ([]lsp.DocumentLink, error) {
		return []lsp.DocumentLink{{Range: lsp.Range{End: lsp.Position{Line: 0, Character: 1}}, Target: "https://x"}}, nil
	}
	h.open("main.go")
	h.tt.Frame()
	drainHarnessPosts(h)
	if countCalls(h.fc.calls(), "documentLink") != 0 {
		t.Fatal("an untrusted workspace asked its server for links")
	}
}

func TestDocumentLinkOpenURL(t *testing.T) {
	h, d := linkHarness(t, func(context.Context, string) ([]lsp.DocumentLink, error) {
		return []lsp.DocumentLink{
			{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 8}, End: lsp.Position{Line: 2, Character: 13}}, Target: "https://pkg.go.dev/fmt"},
		}, nil
	})
	settleLinks(h, d)
	l := d.docLinks[0]
	h.a.openDocLinkTarget(d, &l)
	h.tt.Frame()
	if got := h.tt.OpenedURLs(); !slices.Equal(got, []string{"https://pkg.go.dev/fmt"}) {
		t.Fatalf("opened %q, want the link's URL", got)
	}
}

func TestDocumentLinkOpenFile(t *testing.T) {
	h, d := linkHarness(t, func(context.Context, string) ([]lsp.DocumentLink, error) {
		return []lsp.DocumentLink{}, nil
	})
	target := uri.FromPath(filepath.Join(h.a.root, "lib", "x.go"))
	h.fc.documentLinks = func(context.Context, string) ([]lsp.DocumentLink, error) {
		return []lsp.DocumentLink{
			{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 8}, End: lsp.Position{Line: 2, Character: 13}}, Target: target},
		}, nil
	}
	// Refetch, now that the target names a file.
	h.a.clearDocumentLinks(d)
	h.tt.Frame()
	settleLinks(h, d)
	l := d.docLinks[0]
	h.a.openDocLinkTarget(d, &l)
	h.settle(func() bool {
		return h.a.docs[filepath.Join(h.a.root, "lib", "x.go")] != nil
	})
}

func TestDocumentLinkClickOpensLinkNotDefinition(t *testing.T) {
	h, d := linkHarness(t, func(context.Context, string) ([]lsp.DocumentLink, error) {
		return []lsp.DocumentLink{
			{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 8}, End: lsp.Position{Line: 2, Character: 13}}, Target: "https://pkg.go.dev/fmt"},
		}, nil
	})
	h.fc.def = func(context.Context, string, lsp.Position) ([]lsp.Location, error) {
		return []lsp.Location{{URI: uri.FromPath(d.path), Range: lsp.Range{}}}, nil
	}
	settleLinks(h, d)
	off := d.docLinks[0].start + 1 // inside the link
	h.a.goToDefinition(h.a.focus, d, off)
	h.tt.Frame()
	if got := h.tt.OpenedURLs(); !slices.Equal(got, []string{"https://pkg.go.dev/fmt"}) {
		t.Fatalf("Cmd+click on a link opened %q and did not ask for a definition; calls: %v", got, h.fc.calls())
	}
	if countCalls(h.fc.calls(), "definition") != 0 {
		t.Fatal("a click on a link asked the server for a definition")
	}
}

func TestDocumentLinkClickOffLinkStillDefines(t *testing.T) {
	h, d := linkHarness(t, func(context.Context, string) ([]lsp.DocumentLink, error) {
		return []lsp.DocumentLink{
			{Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 8}, End: lsp.Position{Line: 2, Character: 13}}, Target: "https://pkg.go.dev/fmt"},
		}, nil
	})
	h.fc.def = func(context.Context, string, lsp.Position) ([]lsp.Location, error) {
		return nil, nil
	}
	settleLinks(h, d)
	h.a.goToDefinition(h.a.focus, d, 0) // nowhere near the link
	h.settle(func() bool { return countCalls(h.fc.calls(), "definition") > 0 })
}

func TestDocumentLinksRefetchOnEdit(t *testing.T) {
	var n int
	h, d := linkHarness(t, func(context.Context, string) ([]lsp.DocumentLink, error) {
		n++
		return []lsp.DocumentLink{
			{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 7}}, Target: "https://x"},
		}, nil
	})
	settleLinks(h, d)
	before := countCalls(h.fc.calls(), "documentLink")
	d.buf.Edit(editor.Edit{Text: "// a comment\n"})
	h.tt.Frame()
	h.settle(func() bool {
		h.tt.Frame()
		return countCalls(h.fc.calls(), "documentLink") > before
	})
}

func TestDocumentLinksDropsStaleRange(t *testing.T) {
	h, d := linkHarness(t, func(context.Context, string) ([]lsp.DocumentLink, error) {
		// A range past the text, as a version the server had that is not
		// the snapshot's, is dropped rather than misdrawn.
		return []lsp.DocumentLink{
			{Range: lsp.Range{Start: lsp.Position{Line: 99, Character: 0}, End: lsp.Position{Line: 99, Character: 5}}, Target: "https://x"},
			{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 7}}, Target: "https://ok"},
		}, nil
	})
	settleLinks(h, d)
	if len(d.docLinks) != 1 || d.docLinks[0].target != "https://ok" {
		t.Fatalf("docLinks: %+v", d.docLinks)
	}
}
