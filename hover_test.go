package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
)

func TestHoverCacheAndInvalidation(t *testing.T) {
	_, h := goWorkspace(t)
	h.fc.hover = func(context.Context, string, lsp.Position) (string, *lsp.Range, error) {
		return "Documentation", nil, nil
	}
	d := h.open("a.go")
	st := h.ed(d)
	off := at(d, "名字()")
	h.tt.Frame()
	h.settle(func() bool { return h.a.lspState == lspReady })
	st.OnHover(editor.HoverRequest{Offset: off})
	h.settle(func() bool { return len(h.a.hover.cache) == 1 })
	count := func() int {
		n := 0
		for _, call := range h.fc.calls() {
			if call == "hover" {
				n++
			}
		}
		return n
	}
	if count() != 1 {
		t.Fatalf("first request: %v", h.fc.calls())
	}
	st.OnHover(editor.HoverRequest{Offset: off + 3})
	if count() != 1 {
		t.Fatal("cached hover made another request")
	}
	h.a.applyEvents([]mygo.FileEvent{{Path: "other.go", Op: mygo.FileWrite}})
	if len(h.a.hover.cache) != 0 {
		t.Fatal("workspace change retained cache")
	}
	st.OnHover(editor.HoverRequest{Offset: off})
	h.settle(func() bool { return len(h.a.hover.cache) == 1 })
	if count() != 2 {
		t.Fatalf("workspace change did not requery: %v", h.fc.calls())
	}
	d.buf.Set(strings.Replace(d.buf.String(), "func main", "func changed", 1))
	h.tt.Frame()
	if len(h.a.hover.cache) != 0 {
		t.Fatal("edit retained cache")
	}
}

func TestHoverLocationAndScroll(t *testing.T) {
	_, h := goWorkspace(t)
	d := h.open("a.go")
	h.tt.Frame()
	var lines strings.Builder
	for i := range 60 {
		fmt.Fprintf(&lines, "func Line%d() {}\n", i)
	}
	loc := &hoverLocation{path: "/Users/name/go/pkg/mod/example/editor/state.go", line: 13}
	h.ed(d).ShowHover(at(d, "main"), at(d, "main")+4, "", func(c *ui.Context) {
		hoverContent(c, "```go\n"+lines.String()+"```", loc)
	})
	h.tt.Frame()
	if !h.tt.HasText("state.go") || !h.tt.HasText(":13") || !h.tt.HasText("editor") || h.tt.HasText(loc.path) {
		t.Fatalf("location: %q", h.tt.Texts())
	}
	r, ok := h.tt.Find("Hover documentation")
	if !ok || r.H > 381 {
		t.Fatalf("scroll area: %+v, found %v", r, ok)
	}
	h.tt.Move(r.X+20, r.Y+100)
	h.tt.Frame()
	before := h.tt.Image()
	copyBefore := image.NewRGBA(before.Bounds())
	copy(copyBefore.Pix, before.Pix)
	h.tt.Scroll(r.X+20, r.Y+100, 0, 180)
	h.tt.Frame()
	if bytes.Equal(copyBefore.Pix, h.tt.Image().Pix) {
		t.Fatal("hover content did not scroll")
	}
	h.ed(d).ShowHover(at(d, "main"), at(d, "main")+4, "", func(c *ui.Context) {
		hoverContent(c, "```go\n"+strings.Repeat("LongIdentifier", 30)+"\n```", loc)
	})
	h.tt.Frame()
	r, ok = h.tt.Find("Hover documentation")
	if !ok {
		t.Fatal("missing hover content")
	}
	h.tt.Move(r.X+20, r.Y+50)
	h.tt.Frame()
	before = h.tt.Image()
	copyBefore = image.NewRGBA(before.Bounds())
	copy(copyBefore.Pix, before.Pix)
	h.tt.Scroll(r.X+20, r.Y+50, 200, 0)
	h.tt.Frame()
	if bytes.Equal(copyBefore.Pix, h.tt.Image().Pix) {
		t.Fatal("code did not scroll horizontally")
	}
}

func TestHoverDropsResultAfterEdit(t *testing.T) {
	_, h := goWorkspace(t)
	started, finish := make(chan struct{}), make(chan struct{})
	h.fc.hover = func(context.Context, string, lsp.Position) (string, *lsp.Range, error) {
		close(started)
		<-finish
		return "Old documentation", nil, nil
	}
	d := h.open("a.go")
	h.tt.Frame()
	h.settle(func() bool { return h.a.lspState == lspReady })
	h.drainNow()
	h.ed(d).OnHover(editor.HoverRequest{Offset: at(d, "名字()")})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("hover did not start")
	}
	d.buf.Set(d.buf.String() + "\n// changed\n")
	h.tt.Frame()
	close(finish)
	select {
	case fn := <-h.q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("hover result was not delivered")
	}
	h.drainNow()
	if len(h.a.hover.cache) != 0 {
		t.Fatal("stale result was cached")
	}
}
