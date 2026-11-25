package app

import (
	"context"
	"errors"
	"fmt"
	"runtime"
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
	h.tt.HoldModifiers(parameterHintModifier(runtime.GOOS))
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

func TestParameterHintModifierByPlatform(t *testing.T) {
	for _, tc := range []struct {
		goos string
		want ui.Modifiers
	}{
		{goos: "darwin", want: ui.Alt},
		{goos: "linux", want: ui.Ctrl | ui.Alt},
		{goos: "windows", want: ui.Ctrl | ui.Alt},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			got := parameterHintModifier(tc.goos)
			if got != tc.want {
				t.Fatalf("parameter hint modifier for %s = %v, want %v", tc.goos, got, tc.want)
			}
			if tc.goos != "darwin" && ui.Alt&got == got {
				t.Fatalf("plain Alt satisfies parameter hint modifier on %s", tc.goos)
			}
		})
	}
}

func TestParameterHintsShowOnlyWhilePlatformModifierHeldWithoutEditingSource(t *testing.T) {
	h, d, st := resizeHintsHarness(t, func(context.Context, string, lsp.Range) ([]lsp.InlayHint, error) {
		return resizeParameterHints(), nil
	})
	settleParameterInlays(h, d)
	if got := h.a.editorInlays(d, st); len(got) != 0 {
		t.Fatalf("parameter hints shown before modifier: %+v", got)
	}
	modifier := parameterHintModifier(runtime.GOOS)
	if modifier != ui.Alt {
		h.tt.HoldModifiers(ui.Alt)
		h.tt.Frame()
		if got := h.a.editorInlays(d, st); len(got) != 0 {
			t.Fatalf("parameter hints shown with plain Alt on %s: %+v", runtime.GOOS, got)
		}
	}
	h.tt.HoldModifiers(modifier)
	h.tt.Frame()
	got := h.a.editorInlays(d, st)
	if len(got) != 2 || got[0].Text != " width: " || got[1].Text != " height: " {
		t.Fatalf("parameter hints while modifier held: %+v", got)
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
	if got := h.a.editorInlays(d, st); len(got) != 0 {
		t.Fatalf("parameter hints remained after modifier release: %+v", got)
	}
	if got := d.buf.String(); got != resizeHintSource || d.buf.Modified() {
		t.Fatalf("showing hints changed source: modified=%v, text=%q", d.buf.Modified(), got)
	}
}

func TestParameterHintsRemainUntilChangedBufferReplyArrives(t *testing.T) {
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
			hints := resizeParameterHints()
			for i := range hints {
				hints[i].Position.Line++
				hints[i].Label = "updated " + hints[i].Label
			}
			return hints, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	settleParameterInlays(h, d)
	h.tt.HoldModifiers(parameterHintModifier(runtime.GOOS))
	h.tt.Frame()
	if got := h.a.editorInlays(d, st); len(got) != 2 {
		t.Fatalf("initial parameter hints: %+v", got)
	}
	st.Select(0, 0)
	st.Insert("// prefix\n")
	h.tt.Frame()
	if len(d.parameterInlays) != 2 || len(h.a.editorInlays(d, st)) != 2 {
		t.Fatalf("parameter hints disappeared after buffer change: %+v", d.parameterInlays)
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
	if len(h.a.editorInlays(d, st)) != 2 {
		t.Fatalf("parameter hints disappeared while updated request was pending: %+v", d.parameterInlays)
	}
	close(release)
	settleInlayRequest(h, d)
	if got := h.a.editorInlays(d, st); len(got) != 2 || got[0].Text != " updated width: " || got[0].Offset != strings.Index(d.buf.String(), "320") {
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
	h.tt.HoldModifiers(parameterHintModifier(runtime.GOOS))
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
	h.tt.HoldModifiers(parameterHintModifier(runtime.GOOS))
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
			h.tt.HoldModifiers(parameterHintModifier(runtime.GOOS))
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

// scrollHintsHarness opens a file far taller than the window whose fake
// server hints every line in any range asked.
func scrollHintsHarness(t *testing.T) (*harness, *doc, *editor.State) {
	t.Helper()
	root := writeTree(t, map[string]string{"main.go": bigInlaySource(200)})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.inlayHints = func(_ context.Context, uri string, r lsp.Range) ([]lsp.InlayHint, error) {
		var hints []lsp.InlayHint
		for line := r.Start.Line; line <= r.End.Line; line += 2 {
			hints = append(hints, lsp.InlayHint{Position: lsp.Position{Line: line, Character: 2}, Label: ": int", Kind: 1, PaddingRight: true})
		}
		return hints, nil
	}
	d := h.open("main.go")
	return h, d, h.a.activeEditor()
}

func bigInlaySource(n int) string {
	var sb strings.Builder
	sb.WriteString("package main\n\nfunc compute() int { return 1 }\n\nfunc main() {\n")
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			fmt.Fprintf(&sb, "\tv%d := compute() // line %d\n", i, i)
		} else {
			fmt.Fprintf(&sb, "\t_ = v%d\n", i-1)
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

func TestInlayHintsSurviveScrollWhileNextRequestSettles(t *testing.T) {
	h, d, st := scrollHintsHarness(t)
	settleInlays(h, d)
	before := len(d.languageInlays)
	if before == 0 {
		t.Fatal("no hints before scrolling")
	}
	version := d.buf.Version()
	// Scroll down far enough that the visible range moves: the request
	// for the new lines debounces and then waits on the server. The
	// hints already fetched must stay on screen the whole time, rather
	// than blanking until the reply lands.
	st.SetScrollY(st.LineTop(80))
	h.tt.Frame()
	if len(d.languageInlays) != before {
		t.Fatalf("scrolling blanked the hints: %d -> %d", before, len(d.languageInlays))
	}
	for i := 0; i < 20; i++ {
		h.tt.Frame()
		if d.buf.Version() != version {
			t.Fatal("buffer changed without an edit")
		}
		if len(d.languageInlays) == 0 {
			t.Fatalf("hints flickered off during the scroll settle, frame %d", i)
		}
	}
	settleInlays(h, d)
	if len(d.languageInlays) == 0 {
		t.Fatal("hints never refilled after the scroll settled")
	}
}

func TestInlayHintsMoveOnEdit(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	h, d, st := resizeHintsHarness(t, func(ctx context.Context, _ string, _ lsp.Range) ([]lsp.InlayHint, error) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n == 1 {
			return append(resizeParameterHints(), lsp.InlayHint{Position: lsp.Position{Line: 5, Character: 8}, Label: ": []byte", Kind: 1}), nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	settleParameterInlays(h, d)
	before := d.parameterInlays[0].Offset
	typeBefore := d.languageInlays[0].Offset
	st.Select(0, 0)
	st.Insert("// prefix\n")
	d.diagnosticVersion = d.buf.Version()
	d.diagnostics = []editor.Diagnostic{{Start: 0, Severity: editor.SeverityError, Message: "current diagnostic"}}
	check := func(want int) {
		t.Helper()
		h.tt.Frame()
		if len(d.languageInlays) != 1 || d.languageInlays[0].Offset != typeBefore+want-before {
			t.Fatalf("type hints: %+v", d.languageInlays)
		}
		if len(d.parameterInlays) != 2 || d.parameterInlays[0].Offset != want {
			t.Fatalf("cached hints = %+v, want first offset %d", d.parameterInlays, want)
		}
	}
	check(before + len("// prefix\n"))
	h.tt.HoldModifiers(parameterHintModifier(runtime.GOOS))
	for i := 0; i < 3; i++ {
		check(before + len("// prefix\n"))
	}
	if got := h.a.editorInlays(d, st); len(got) != 4 || got[1].Offset != before+len("// prefix\n") || got[3].Offset != len("// prefix") {
		t.Fatalf("visible hints: %+v", got)
	}
	h.tt.HoldModifiers(0)
	check(before + len("// prefix\n"))
	if got := h.a.editorInlays(d, st); len(got) != 2 || got[1].Offset != len("// prefix") {
		t.Fatalf("hidden hints: %+v", got)
	}
	h.tt.HoldModifiers(parameterHintModifier(runtime.GOOS))
	check(before + len("// prefix\n"))
	if got := h.a.editorInlays(d, st); len(got) != 4 || got[1].Offset != before+len("// prefix\n") || got[3].Offset != len("// prefix") {
		t.Fatalf("re-enabled hints: %+v", got)
	}
	if !d.buf.Undo() {
		t.Fatal("insert was not undoable")
	}
	check(before)
}

func TestInlayHintsRecoverAfterEditLogGap(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	requests := 0
	h, d, _ := resizeHintsHarness(t, func(ctx context.Context, _ string, _ lsp.Range) ([]lsp.InlayHint, error) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n == 1 {
			return resizeParameterHints(), nil
		}
		select {
		case <-release:
			hints := resizeParameterHints()
			hints[0].Label = "recovered:"
			return hints, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	settleParameterInlays(h, d)
	// Append beyond the hinted lines without letting the editor consume its edit log.
	for i := 0; i < 300; i++ {
		end := d.buf.Len()
		d.buf.Edit(editor.Edit{Start: end, End: end, Text: " "})
	}
	h.tt.Frame()
	if len(d.parameterInlays) != 0 || len(d.languageInlays) != 0 {
		t.Fatalf("hints survived unavailable edit history: %+v", d.parameterInlays)
	}
	h.settle(func() bool {
		h.tt.Frame()
		mu.Lock()
		defer mu.Unlock()
		return requests >= 2
	})
	close(release)
	settleParameterInlays(h, d)
	if d.parameterInlays[0].Text != " recovered: " {
		t.Fatalf("fresh hints not installed: %+v", d.parameterInlays)
	}
	for i := 0; i < 3; i++ {
		h.tt.Frame()
		if len(d.parameterInlays) != 2 {
			t.Fatal("fresh hints cleared on a later frame")
		}
	}
}

func TestInlayHintsReplayMultipleEditsBeforeFrame(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	h, d, _ := resizeHintsHarness(t, func(ctx context.Context, _ string, _ lsp.Range) ([]lsp.InlayHint, error) {
		mu.Lock()
		requests++
		n := requests
		mu.Unlock()
		if n == 1 {
			return resizeParameterHints(), nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	settleParameterInlays(h, d)
	before := []int{d.parameterInlays[0].Offset, d.parameterInlays[1].Offset}
	d.buf.Edit(editor.Edit{Text: "// prefix\n"})
	at := strings.Index(d.buf.String(), "180")
	d.buf.Edit(editor.Edit{Start: at, End: at, Text: "  "})
	for i := 0; i < 3; i++ {
		h.tt.Frame()
		if len(d.parameterInlays) != 2 || d.parameterInlays[0].Offset != before[0]+len("// prefix\n") || d.parameterInlays[1].Offset != before[1]+len("// prefix\n")+2 {
			t.Fatalf("frame %d: hints did not replay both edits exactly once: %+v", i, d.parameterInlays)
		}
	}
}
