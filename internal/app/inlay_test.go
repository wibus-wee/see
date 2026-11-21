package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
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

func settleParameterInlays(h *harness, d *doc) {
	h.settle(func() bool {
		h.tt.Frame()
		return len(d.parameterInlays) > 0
	})
}

func settleInlayRequest(h *harness, d *doc) {
	h.settle(func() bool {
		h.tt.Frame()
		return d.inlayCancel == nil
	})
}

const resizeHintSource = "package main\n\nfunc resize(img []byte, width, height int) {}\n\nfunc main() {\n\tvar img []byte\n\tresize(img, 320, 180)\n}\n"

func resizeParameterHints() []lsp.InlayHint {
	line := "\tresize(img, 320, 180)"
	return []lsp.InlayHint{
		{Position: lsp.Position{Line: 6, Character: strings.Index(line, "320")}, Label: "width:", Kind: 2, PaddingLeft: true, PaddingRight: true},
		{Position: lsp.Position{Line: 6, Character: strings.Index(line, "180")}, Label: "height:", Kind: 2, PaddingLeft: true, PaddingRight: true},
	}
}

func resizeHintsHarness(t *testing.T, hints func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error)) (*harness, *doc, *editor.State) {
	t.Helper()
	root := writeTree(t, map[string]string{"main.go": resizeHintSource})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.inlayHints = hints
	d := h.open("main.go")
	return h, d, h.a.activeEditor()
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
	if len(d.languageInlays) != 1 || d.languageInlays[0].Text != " x " || len(d.parameterInlays) != 1 || d.parameterInlays[0].Text != "name:" {
		t.Fatalf("language inlays: %+v; parameter inlays: %+v", d.languageInlays, d.parameterInlays)
	}
}

func TestTypeHintPreferenceDoesNotControlParameterHints(t *testing.T) {
	h, d, _ := inlayHarness(t, func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) {
		return []lsp.InlayHint{
			{Position: lsp.Position{Line: 5, Character: 4}, Label: ": int", Kind: 1},
			{Position: lsp.Position{Line: 5, Character: 4}, Label: "width:", Kind: 2},
		}, nil
	})
	settleInlays(h, d)
	st := h.a.activeEditor()
	h.tt.HoldModifiers(ui.Alt)
	h.tt.Frame()
	if got := h.a.editorInlays(d, st); len(got) != 2 || got[0].Text != ": int" || got[1].Text != "width:" {
		t.Fatalf("hints while enabled: %+v", got)
	}
	calls := countCalls(h.fc.calls(), "inlayHint")
	h.a.toggleTypeHints()
	h.tt.Frame()
	if !h.a.cfg.HideTypeHints {
		t.Fatal("type hints remain enabled")
	}
	if got := h.a.editorInlays(d, st); len(got) != 1 || got[0].Text != "width:" {
		t.Fatalf("type preference affected parameter hints: %+v", got)
	}
	h.a.toggleTypeHints()
	h.tt.Frame()
	if got := h.a.editorInlays(d, st); len(got) != 2 || got[0].Text != ": int" || got[1].Text != "width:" {
		t.Fatalf("hints after re-enabling types: %+v", got)
	}
	if countCalls(h.fc.calls(), "inlayHint") != calls {
		t.Fatal("type preference caused an unnecessary hint request")
	}
}

func TestParameterHintsShowOnlyWhileAltHeldWithoutEditingSource(t *testing.T) {
	h, d, st := resizeHintsHarness(t, func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) {
		return resizeParameterHints(), nil
	})
	settleParameterInlays(h, d)
	if got := h.a.editorInlays(d, st); len(got) != 0 {
		t.Fatalf("parameter hints shown before Alt: %+v", got)
	}
	h.tt.HoldModifiers(ui.Alt)
	h.tt.Frame()
	got := h.a.editorInlays(d, st)
	if len(got) != 2 || got[0].Text != " width: " || got[1].Text != " height: " {
		t.Fatalf("parameter hints while Alt held: %+v", got)
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
	if got := h.a.editorInlays(d, st); len(got) != 0 {
		t.Fatalf("parameter hints remained after Alt release: %+v", got)
	}
	if got := d.buf.String(); got != resizeHintSource || d.buf.Modified() {
		t.Fatalf("showing hints changed source: modified=%v, text=%q", d.buf.Modified(), got)
	}
}

func TestParameterHintsClearUntilChangedBufferReplyArrives(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	requests := 0
	h, d, st := resizeHintsHarness(t, func(ctx context.Context, _ string, _ lsp.Range) ([]lsp.InlayHint, error) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n == 1 {
			return resizeParameterHints(), nil
		}
		once.Do(func() { close(started) })
		select {
		case <-release:
			return resizeParameterHints(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	settleParameterInlays(h, d)
	h.tt.HoldModifiers(ui.Alt)
	h.tt.Frame()
	if got := h.a.editorInlays(d, st); len(got) != 2 {
		t.Fatalf("initial parameter hints: %+v", got)
	}
	d.buf.Set(strings.Replace(d.buf.String(), "320", "640", 1))
	h.tt.Frame()
	if len(d.parameterInlays) != 0 || len(h.a.editorInlays(d, st)) != 0 {
		t.Fatalf("old parameter hints remained after buffer change: %+v", d.parameterInlays)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-started:
			goto requested
		case fn := <-h.q:
			fn()
		case <-deadline:
			t.Fatal("updated hint request never started")
		case <-time.After(10 * time.Millisecond):
			h.tt.Frame()
		}
	}
requested:
	if len(h.a.editorInlays(d, st)) != 0 {
		t.Fatalf("stale parameter hints appeared while updated request was pending: %+v", d.parameterInlays)
	}
	close(release)
	settleParameterInlays(h, d)
	if got := h.a.editorInlays(d, st); len(got) != 2 {
		t.Fatalf("updated parameter hints did not arrive: %+v", got)
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
		return []lsp.InlayHint{{Position: lsp.Position{Line: 5, Character: 4}, Label: "width:", Kind: 2}}, nil
	})
	h.tt.HoldModifiers(ui.Alt)
	h.tt.Frame()
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
	if len(d.parameterInlays) != 0 {
		t.Fatalf("stale parameter hints applied: %+v", d.parameterInlays)
	}
}

func TestParameterHintsDisappearWhenWorkspaceTrustIsRemoved(t *testing.T) {
	h, d, st := resizeHintsHarness(t, func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) {
		return resizeParameterHints(), nil
	})
	settleParameterInlays(h, d)
	h.tt.HoldModifiers(ui.Alt)
	h.tt.Frame()
	if got := h.a.editorInlays(d, st); len(got) != 2 {
		t.Fatalf("parameter hints before trust removal: %+v", got)
	}
	h.a.cfg.Trusted = nil
	if got := h.a.editorInlays(d, st); len(got) != 0 {
		t.Fatalf("parameter hints exposed after trust removal: %+v", got)
	}
	h.tt.Frame()
	if len(d.parameterInlays) != 0 {
		t.Fatalf("cached parameter hints survived trust removal: %+v", d.parameterInlays)
	}
}

func TestParameterHintsUnsupportedEmptyAndErrorsAreSilent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hints func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error)
	}{
		{name: "unsupported"},
		{name: "empty", hints: func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) { return nil, nil }},
		{name: "error", hints: func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) {
			return nil, errors.New("unsupported")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, d, st := resizeHintsHarness(t, tc.hints)
			settleInlayRequest(h, d)
			h.tt.HoldModifiers(ui.Alt)
			h.tt.Frame()
			if got := h.a.editorInlays(d, st); len(got) != 0 {
				t.Fatalf("unsupported hints became visible: %+v", got)
			}
			if len(d.languageInlays) != 0 || len(d.parameterInlays) != 0 {
				t.Fatalf("unsupported hints populated lanes: language=%+v parameters=%+v", d.languageInlays, d.parameterInlays)
			}
		})
	}
}
