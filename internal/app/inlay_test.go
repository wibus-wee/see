package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

// inlayHarness opens a Go file whose fake server answers hints.
func inlayHarness(t *testing.T, hints func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error)) (*harness, *doc, *editor.State) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tx := compute()\n\tfmt.Println(x)\n}\n\nfunc compute() int { return 1 }\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.inlayHints = hints
	d := h.open("main.go")
	return h, d, h.a.activeEditor()
}

func settleInlays(h *harness, d *doc) {
	h.settle(func() bool {
		h.tt.Frame()
		return len(d.languageInlays) > 0
	})
}

func TestInlayHintsFillLanguageLane(t *testing.T) {
	var mu sync.Mutex
	var gotRange lsp.Range
	h, d, _ := inlayHarness(t, func(_ context.Context, uri string, r lsp.Range) ([]lsp.InlayHint, error) {
		mu.Lock()
		gotRange = r
		mu.Unlock()
		return []lsp.InlayHint{
			{Position: lsp.Position{Line: 5, Character: 4}, Label: ": int", Kind: 1, PaddingRight: true},
		}, nil
	})
	settleInlays(h, d)
	snap := d.buf.Snapshot()
	want := snap.LineStart(5) + lsp.ColumnFromLSP(snap.Line(5), 4, h.fc.enc)
	if len(d.languageInlays) != 1 || d.languageInlays[0].Text != ": int " || d.languageInlays[0].Offset != want {
		t.Fatalf("languageInlays: %+v (want offset %d)", d.languageInlays, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if gotRange.Start.Line != 0 {
		t.Fatalf("request range starts at line %d, want the first visible line", gotRange.Start.Line)
	}
}

func TestInlayHintsPaddingAndParts(t *testing.T) {
	h, d, _ := inlayHarness(t, func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) {
		return []lsp.InlayHint{
			{Position: lsp.Position{Line: 5, Character: 2}, Label: "x", Kind: 1, PaddingLeft: true, PaddingRight: true},
			{Position: lsp.Position{Line: 5, Character: 20}, Label: "name:", Kind: 2},
		}, nil
	})
	settleInlays(h, d)
	if len(d.languageInlays) != 2 || d.languageInlays[0].Text != " x " || d.languageInlays[1].Text != "name:" {
		t.Fatalf("languageInlays: %+v", d.languageInlays)
	}
}

func TestInlayHintsToggleEmptiesLane(t *testing.T) {
	h, d, _ := inlayHarness(t, func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) {
		return []lsp.InlayHint{{Position: lsp.Position{Line: 5, Character: 4}, Label: ": int"}}, nil
	})
	settleInlays(h, d)
	h.a.toggleTypeHints()
	if len(d.languageInlays) != 0 || !h.a.cfg.HideTypeHints {
		t.Fatalf("toggle left hints: %+v", d.languageInlays)
	}
	before := countCalls(h.fc.calls(), "inlayHint")
	h.tt.Frame()
	if countCalls(h.fc.calls(), "inlayHint") != before {
		t.Fatal("hints requested while switched off")
	}
}

func TestInlayHintsStaleReplyDropped(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h, d, _ := inlayHarness(t, func(ctx context.Context, _ string, _ lsp.Range) ([]lsp.InlayHint, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []lsp.InlayHint{{Position: lsp.Position{Line: 5, Character: 4}, Label: ": int"}}, nil
	})
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-started:
			goto requested
		case fn := <-h.q:
			fn()
		case <-deadline:
			t.Fatal("hint request never started")
		case <-time.After(10 * time.Millisecond):
			h.tt.Frame()
		}
	}
requested:
	// The buffer moves on before the reply lands: it must not apply.
	d.buf.Set(strings.Replace(d.buf.String(), "compute()", "compute2()", 1))
	close(release)
	time.Sleep(50 * time.Millisecond) // the fetch's posts land behind the reply
	drainHarnessPosts(h)
	h.tt.Frame()
	if len(d.languageInlays) != 0 {
		t.Fatalf("stale hints applied: %+v", d.languageInlays)
	}
}
