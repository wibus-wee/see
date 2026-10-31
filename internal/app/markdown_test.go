package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestMarkdownPreview(t *testing.T) {
	root := writeTree(t, map[string]string{"README.md": "# Hello preview\n\n**Bold text**\n", "other.go": "package other\n"})
	h := newHarness(t, root)
	d := h.open("README.md")
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyV)
	h.tt.Frame()
	if h.a.focus.active != markdownTab(d.path) || !h.a.focus.has(d.path) || !h.tt.HasText("Hello preview") || !h.tt.HasText("Bold text") {
		t.Fatalf("preview not shown: %q", h.tt.Texts())
	}
	d.buf.Set("# Updated content\n")
	h.tt.Frame()
	if !h.tt.HasText("Updated content") || h.tt.HasText("Hello preview") {
		t.Fatalf("preview did not update: %q", h.tt.Texts())
	}
	h.click("Show Markdown Source")
	h.tt.Frame()
	if h.a.focus.active != d.path || !h.a.focus.has(markdownTab(d.path)) {
		t.Fatal("source button did not retain the preview tab")
	}
	h.click("Open Markdown Preview")
	h.tt.Frame()
	h.a.openIn(h.a.focus, d.path, goTo{pos: &editor.Pos{Line: 0}})
	h.tt.Frame()
	if h.a.focus.active != d.path {
		t.Fatal("source navigation did not reveal editor")
	}
	h.open("other.go")
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyV)
	if h.a.focus.has(markdownTab(filepath.Join(root, "other.go"))) {
		t.Fatal("preview opened for Go file")
	}
}

func TestMarkdownImages(t *testing.T) {
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	requests := make(chan string, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data.Bytes())
	}))
	defer server.Close()
	root := writeTree(t, map[string]string{"README.md": "<img alt=\"Screenshot\" src=\"" + server.URL + "/image\">\n"})
	if err := os.WriteFile(filepath.Join(root, "local image.png"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	load := markdownImages(root)
	for _, src := range []string{server.URL + "/image", "local%20image.png"} {
		if _, err := load(src); err != nil {
			t.Fatalf("load %q: %v", src, err)
		}
	}
	if _, err := load(server.URL + "/missing"); err == nil {
		t.Fatal("missing remote image succeeded")
	}
	<-requests
	<-requests
	h := newHarness(t, root)
	d := h.open("README.md")
	h.a.openMarkdownTab(h.a.focus)
	h.tt.Frame()
	select {
	case path := <-requests:
		if path != "/image" {
			t.Fatalf("requested %q", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HTML image was not requested")
	}
	h.tt.Frame()
	if _, ok := h.tt.Find("Screenshot"); !ok {
		t.Fatalf("HTML image missing from preview: %q", h.tt.Texts())
	}
	d.buf.Set("![Markdown image](" + server.URL + "/another)\n")
	h.tt.Frame()
	select {
	case path := <-requests:
		if path != "/another" {
			t.Fatalf("requested %q", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Markdown image was not requested")
	}
	h.tt.Frame()
	if _, ok := h.tt.Find("Markdown image"); !ok {
		t.Fatalf("Markdown image missing from preview: %q", h.tt.Texts())
	}
}

func TestStandaloneHTMLImage(t *testing.T) {
	src, alt, ok := standaloneHTMLImage(`<IMG alt="A &amp; B" src="image.png">`)
	if !ok || src != "image.png" || alt != "A & B" {
		t.Fatalf("image = %q, %q, %v", src, alt, ok)
	}
	for _, input := range []string{`<div><img src="x"></div>`, `text <img src="x">`, `<img src="x"><img src="y">`, `<img>`} {
		if _, _, ok := standaloneHTMLImage(input); ok {
			t.Errorf("accepted %s", input)
		}
	}
}

func TestMarkdownPreviewPerGroup(t *testing.T) {
	root := writeTree(t, map[string]string{"README.md": "# Heading\n"})
	h := newHarness(t, root)
	d := h.open("README.md")
	first := h.a.focus
	h.a.openMarkdownTab(first)
	key := markdownTab(d.path)
	h.a.moveTab(first, key, first, sideRight, 0)
	second := h.a.focus
	if !first.has(d.path) || first.has(key) || !second.has(key) || second.active != key {
		t.Fatal("preview did not move to its own group")
	}
	d.buf.Set("# Unsaved change\n")
	h.tt.Frame()
	if !h.tt.HasText("Unsaved change") {
		t.Fatal("preview did not follow unsaved edits")
	}
	h.a.closeTab(second, key)
	if h.a.docs[d.path] == nil || !first.has(d.path) {
		t.Fatal("closing preview released its source")
	}
}

func TestMarkdownPreviewSessionAndCloseSource(t *testing.T) {
	root := writeTree(t, map[string]string{"README.md": "# Hello\n"})
	h := newHarness(t, root)
	d := h.open("README.md")
	g := h.a.focus
	h.a.openMarkdownTab(g)
	key := markdownTab(d.path)
	h.a.closeTab(g, d.path)
	if !g.has(key) || h.a.docs[d.path] == nil {
		t.Fatal("closing source released the preview document")
	}
	h.tt.Frame()
	if _, ok := h.tt.Find("Open files"); ok {
		t.Fatal("single preview unnecessarily shows a tab bar")
	}
	h.a.captureSession()
	h.a.openWorkspace(root)
	g = h.a.focus
	if !g.has(key) || g.has(d.path) || g.active != key {
		t.Fatalf("preview session not restored: %+v", g.order)
	}
	h.settle(func() bool { return h.a.docs[d.path] != nil && !h.a.docs[d.path].loading })
	if !h.tt.HasText("Hello") {
		t.Fatalf("restored preview missing: %q", h.tt.Texts())
	}
}

func TestMoveLastPreviewTabBackIntoSourceGroup(t *testing.T) {
	root := writeTree(t, map[string]string{"README.md": "# Hello\n"})
	h := newHarness(t, root)
	d := h.open("README.md")
	left := h.a.focus
	h.a.openMarkdownTab(left)
	key := markdownTab(d.path)
	h.a.moveTab(left, key, left, sideRight, 0)
	h.tt.Frame()
	src, ok := h.tt.Find("README.md Preview")
	if !ok {
		t.Fatalf("preview tab missing: %q", h.tt.Texts())
	}
	dst, ok := h.tt.Find("Close README.md")
	if !ok {
		t.Fatal("source tab missing")
	}
	h.tt.Press(src.X+src.W/2, src.Y+src.H/2)
	h.tt.Move(src.X-40, src.Y+src.H/2)
	h.tt.Move(dst.X+35, dst.Y+dst.H/2)
	h.tt.Release(dst.X+35, dst.Y+dst.H/2)
	h.tt.Frame()
	h.tt.Frame()
	if len(h.a.groups()) != 1 || !left.has(d.path) || !left.has(key) {
		t.Fatalf("tabs were not merged: groups=%d tabs=%q", len(h.a.groups()), left.order)
	}
}
