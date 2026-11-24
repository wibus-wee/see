package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

func TestCompletionBadgeIsSquareAndUsesCompactGlyphs(t *testing.T) {
	var bounds ui.Rect
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Width(30).Children(func() {
			bounds = completionBadgeView(c, 16, editor.CompletionModule).Bounds()
			ui.Text(c, "long label").Shrink(1)
		})
	}, 80, 40)
	tt.Frame()
	tt.Frame()
	if bounds.W != 16 || bounds.H != 16 || bounds.W != bounds.H {
		t.Fatalf("completion badge bounds = %+v, want fixed 16px square", bounds)
	}
	if !tt.HasText("M") || tt.HasText("{}") {
		t.Fatalf("module badge should be a compact See-style glyph: %q", tt.Texts())
	}
	if glyph, _ := completionBadge(editor.CompletionFunction); glyph != "f" {
		t.Fatalf("function badge = %q", glyph)
	}
}

func TestCompletionRendererShowsLabelDetailAndSelectedDocumentation(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{
			Label: "abi", Kind: 10,
			LabelDetails:  lsp.CompletionItemLabelDetails{Detail: "boolean"},
			Documentation: "selected property docs",
		}}}, nil
	}
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), ", next")))
	h.tt.Frame()
	h.tt.Type("ab")
	waitForCompletion(t, h, "abi")
	label, labelOK := h.tt.Find("abi")
	detail, detailOK := h.tt.Find("boolean")
	docs, docsOK := h.tt.Find("selected property docs")
	if !labelOK || !detailOK || !docsOK || detail.X <= label.X || docs.Y <= detail.Y || !h.tt.HasText("F") {
		t.Fatalf("completion row/docs not rendered as expected: texts=%q label=%+v detail=%+v docs=%+v", h.tt.Texts(), label, detail, docs)
	}
}

func TestCompletionPropertyDetailsFitMeasuredBounds(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	details := []string{"(property) abi: number", "(property) abiVersion?: string"}
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{
			{Label: "abi", Kind: 10, Detail: details[0]},
			{Label: "abiVersion?", Kind: 10, Detail: details[1]},
		}}, nil
	}
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), ", next")))
	h.tt.Frame()
	h.tt.Type("ab")
	waitForCompletion(t, h, "abiVersion?")

	fontSize := max(st.Font.Size-1, 1)
	var measured [2]float32
	measure := ui.NewTester(func(c *ui.Context) {
		font := c.Theme().Font
		for i, detail := range details {
			measured[i], _ = c.MeasureText(0, ui.Span{Text: detail, Font: font, Size: fontSize})
		}
	}, 1100, 300)
	measure.Frame()
	for i, detail := range details {
		bounds, ok := h.tt.Find(detail)
		if !ok || bounds.W+0.5 < measured[i] {
			t.Fatalf("completion detail %q bounds=%+v measured=%0.1f texts=%q", detail, bounds, measured[i], h.tt.Texts())
		}
	}
	if popup, ok := h.tt.Find("Completions"); !ok || popup.W > 600 {
		t.Fatalf("completion popup bounds exceed the 600 DIPs cap: %+v, found=%v", popup, ok)
	}
}

func TestCompletionResolveSelectionChangesDoNotResetMenu(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	started := make(chan string, 2)
	returned := map[string]chan struct{}{"abi": make(chan struct{}), "abc": make(chan struct{})}
	release := map[string]chan struct{}{"abi": make(chan struct{}), "abc": make(chan struct{})}
	h.fc.completionResolve = func(_ context.Context, item lsp.CompletionItem) (*lsp.CompletionItem, error) {
		started <- item.Label
		<-release[item.Label]
		close(returned[item.Label])
		return &lsp.CompletionItem{Detail: item.Label + " type", Documentation: item.Label + " docs"}, nil
	}
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{
			{Label: "abi", Kind: 10, Raw: json.RawMessage(`{"label":"abi","kind":10,"data":{"id":"abi"}}`)},
			{Label: "abc", Kind: 10, Raw: json.RawMessage(`{"label":"abc","kind":10,"data":{"id":"abc"}}`)},
		}}, nil
	}
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), ", next")))
	h.tt.Frame()
	h.tt.Type("a")
	waitForCompletion(t, h, "abi")
	first := waitForResolveStart(t, h, started)
	if first != "abi" && first != "abc" {
		t.Fatalf("first selected item = %q", first)
	}
	for range 3 {
		h.tt.Frame()
		drainHarnessPosts(h)
	}
	if countCalls(h.fc.calls(), "completionItem/resolve") != 1 {
		t.Fatal("selected completion was resolved more than once per result")
	}
	h.tt.Key(0, ui.KeyDown)
	h.tt.Frame()
	second := waitForResolveStart(t, h, started)
	if second == first {
		t.Fatalf("Down did not change selected completion: %q", second)
	}
	cs := h.a.completionFor(h.a.focus)
	seq := cs.seq
	firstKey := completionKeyForLabel(cs, first)
	secondKey := completionKeyForLabel(cs, second)

	close(release[first])
	waitForResolveReturn(t, h, returned[first])
	if _, ok := cs.resolved[firstKey]; ok || h.tt.HasText(first+" docs") {
		t.Fatalf("stale selected-item resolve was displayed: texts=%q resolved=%+v", h.tt.Texts(), cs.resolved)
	}

	close(release[second])
	waitForResolveReturn(t, h, returned[second])
	waitForAppCondition(t, h, func() bool { _, ok := cs.resolved[secondKey]; return ok })
	if cs.seq != seq || !cs.visible || cs.resolveSelected != secondKey ||
		!h.tt.HasText(second+" docs") || !h.tt.HasText("abi") || !h.tt.HasText("abc") {
		t.Fatalf("resolved metadata reset or hid the menu: seq=%d/%d selected=%q texts=%q", cs.seq, seq, cs.resolveSelected.Label, h.tt.Texts())
	}
}

func TestCompletionResolvePrefersMainDetailOverLabelDetails(t *testing.T) {
	for _, test := range []struct {
		name         string
		resolved     *lsp.CompletionItem
		wantMainType bool
		wantFallback bool
	}{
		{
			name: "resolved field type with existing docs",
			resolved: &lsp.CompletionItem{
				Detail: "(property) abi: number", Documentation: "resolved docs",
			},
			wantMainType: true,
		},
		{
			name:         "label details fallback when resolve has no main detail",
			resolved:     &lsp.CompletionItem{Documentation: "resolved docs"},
			wantFallback: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
			var resolves atomic.Int32
			h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
				return &lsp.CompletionResult{Items: []lsp.CompletionItem{{
					Label: "abi", Kind: 10, LabelDetails: lsp.CompletionItemLabelDetails{Detail: "boolean"},
					Documentation: "existing docs",
					Raw:           json.RawMessage("{\"label\":\"abi\",\"kind\":10,\"labelDetails\":{\"detail\":\"boolean\"},\"documentation\":\"existing docs\",\"data\":{\"id\":\"abi\"}}"),
				}}}, nil
			}
			h.fc.completionResolve = func(context.Context, lsp.CompletionItem) (*lsp.CompletionItem, error) {
				resolves.Add(1)
				return test.resolved, nil
			}
			st := h.a.activeEditor()
			st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), ", next")))
			h.tt.Frame()
			h.tt.Type("a")
			waitForCompletion(t, h, "abi")
			cs := h.a.completionFor(h.a.focus)
			key := completionKeyForLabel(cs, "abi")
			waitForAppCondition(t, h, func() bool { return cs.resolveAttempted[key] })

			gotMainType := h.tt.HasText("(property) abi: number")
			gotFallback := h.tt.HasText("boolean")
			if resolves.Load() != 1 || gotMainType != test.wantMainType || gotFallback != test.wantFallback ||
				!h.tt.HasText("existing docs") || h.tt.HasText("resolved docs") {
				t.Fatalf("resolve=%d mainType=%v fallback=%v texts=%q", resolves.Load(), gotMainType, gotFallback, h.tt.Texts())
			}
		})
	}
}

func TestCompletionResolveEscapeRejectsLateReply(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	started := make(chan struct{}, 1)
	returned := make(chan struct{})
	release := make(chan struct{})
	h.fc.completionResolve = func(context.Context, lsp.CompletionItem) (*lsp.CompletionItem, error) {
		started <- struct{}{}
		<-release
		close(returned)
		return &lsp.CompletionItem{Detail: "late type", Documentation: "late docs"}, nil
	}
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{
			Label: "abi", Kind: 10, Raw: json.RawMessage(`{"label":"abi","kind":10,"data":{"id":"abi"}}`),
		}}}, nil
	}
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), ", next")))
	h.tt.Frame()
	h.tt.Type("a")
	waitForCompletion(t, h, "abi")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("selected completion was not resolved")
	}
	cs := h.a.completionFor(h.a.focus)
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if cs.visible || cs.resolvePending {
		t.Fatal("Escape did not cancel completion resolve and hide the menu")
	}
	close(release)
	waitForResolveReturn(t, h, returned)
	waitForAppCondition(t, h, func() bool { return len(h.q) == 0 })
	if cs.visible || len(cs.resolved) != 0 || h.tt.HasText("abi") || h.tt.HasText("late docs") {
		t.Fatalf("late resolve reopened or decorated escaped completion: texts=%q resolved=%+v", h.tt.Texts(), cs.resolved)
	}
}

func TestCompletionResolveCaretChangeRejectsLateMetadata(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	started := make(chan struct{}, 1)
	returned := make(chan struct{})
	release := make(chan struct{})
	h.fc.completionResolve = func(context.Context, lsp.CompletionItem) (*lsp.CompletionItem, error) {
		started <- struct{}{}
		<-release
		close(returned)
		return &lsp.CompletionItem{Detail: "stale type", Documentation: "stale docs"}, nil
	}
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{
			Label: "abi", Kind: 10, Raw: json.RawMessage(`{"label":"abi","kind":10,"data":{"id":"abi"}}`),
		}}}, nil
	}
	st := h.a.activeEditor()
	originalOffset := strings.Index(d.buf.String(), ", next")
	st.GoTo(d.buf.Pos(originalOffset))
	h.tt.Frame()
	h.tt.Type("a")
	waitForCompletion(t, h, "abi")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("selected completion was not resolved")
	}
	cs := h.a.completionFor(h.a.focus)
	st.GoTo(d.buf.Pos(0))
	h.tt.Frame()
	if cs.resolvePending {
		t.Fatal("caret movement did not cancel completion resolve")
	}
	close(release)
	waitForResolveReturn(t, h, returned)
	if len(cs.resolved) != 0 || h.tt.HasText("stale docs") {
		t.Fatalf("caret-stale metadata was displayed: texts=%q resolved=%+v", h.tt.Texts(), cs.resolved)
	}
}

func TestCompletionCompleteResultsRefreshOnTypingAndRejectLateResolve(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	var completionCalls atomic.Int32
	oldStarted, freshStarted := make(chan struct{}, 1), make(chan struct{}, 1)
	oldReturned, freshReturned := make(chan struct{}, 1), make(chan struct{}, 1)
	oldRelease, freshRelease := make(chan struct{}, 1), make(chan struct{}, 1)
	defer func() {
		select {
		case oldRelease <- struct{}{}:
		default:
		}
		select {
		case freshRelease <- struct{}{}:
		default:
		}
	}()
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		if completionCalls.Add(1) == 1 {
			return &lsp.CompletionResult{Items: []lsp.CompletionItem{{
				Label: "abi", Kind: 10, Raw: json.RawMessage("{\"label\":\"abi\",\"kind\":10,\"data\":{\"id\":\"first\"}}"),
			}}}, nil
		}
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{
			Label: "abi", Kind: 10, Raw: json.RawMessage("{\"label\":\"abi\",\"kind\":10,\"data\":{\"id\":\"fresh\"}}"),
		}}}, nil
	}
	h.fc.completionResolve = func(_ context.Context, item lsp.CompletionItem) (*lsp.CompletionItem, error) {
		if strings.Contains(string(item.Raw), "\"first\"") {
			oldStarted <- struct{}{}
			<-oldRelease
			oldReturned <- struct{}{}
			return &lsp.CompletionItem{Detail: "stale type", Documentation: "stale docs"}, nil
		}
		freshStarted <- struct{}{}
		<-freshRelease
		freshReturned <- struct{}{}
		return &lsp.CompletionItem{Detail: "fresh type", Documentation: "fresh docs"}, nil
	}
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), ", next")))
	h.tt.Frame()
	h.tt.Type("a")
	waitForCompletion(t, h, "abi")
	select {
	case <-oldStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first selected completion resolve did not start")
	}

	h.tt.Type("b")
	deadline := time.Now().Add(5 * time.Second)
	for countCalls(h.fc.calls(), "completion") < 2 {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatalf("typing the next character did not request fresh complete results: calls=%v texts=%q", h.fc.calls(), h.tt.Texts())
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		h.tt.Frame()
		drainHarnessPosts(h)
		select {
		case <-freshStarted:
			goto freshResolveStarted
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("fresh selected completion did not resolve: calls=%v texts=%q", h.fc.calls(), h.tt.Texts())
		}
		time.Sleep(10 * time.Millisecond)
	}

freshResolveStarted:
	if !strings.Contains(d.buf.String(), "ab") {
		t.Fatalf("successive typing did not update the document: %q", d.buf.String())
	}

	freshRelease <- struct{}{}
	waitForResolveReturn(t, h, freshReturned)
	waitForAppCondition(t, h, func() bool { return h.tt.HasText("fresh type") && h.tt.HasText("fresh docs") })
	label, labelOK := h.tt.Find("abi")
	detail, detailOK := h.tt.Find("fresh type")
	docs, docsOK := h.tt.Find("fresh docs")
	if !labelOK || !detailOK || !docsOK || detail.X <= label.X || docs.Y <= detail.Y {
		t.Fatalf("fresh completion metadata is not visible in the detail/docs positions: texts=%q label=%+v detail=%+v docs=%+v", h.tt.Texts(), label, detail, docs)
	}
	oldRelease <- struct{}{}
	waitForResolveReturn(t, h, oldReturned)
	if !h.tt.HasText("fresh type") || !h.tt.HasText("fresh docs") || h.tt.HasText("stale type") || h.tt.HasText("stale docs") {
		t.Fatalf("late resolve from the old prefix replaced fresh metadata: texts=%q", h.tt.Texts())
	}
}

func TestCompletionResolveErrorsAndUnsupportedKeepSuggestions(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(*fakeClient)
	}{
		{name: "error", set: func(fc *fakeClient) {
			fc.completionResolve = func(context.Context, lsp.CompletionItem) (*lsp.CompletionItem, error) {
				return nil, errors.New("resolve failed")
			}
		}},
		{name: "unsupported", set: func(*fakeClient) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
			test.set(h.fc)
			h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
				return &lsp.CompletionResult{Items: []lsp.CompletionItem{{
					Label: "abi", Kind: 10, Raw: json.RawMessage(`{"label":"abi","kind":10,"data":{"id":"abi"}}`),
				}}}, nil
			}
			st := h.a.activeEditor()
			st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), ", next")))
			h.tt.Frame()
			h.tt.Type("a")
			waitForCompletion(t, h, "abi")
			cs := h.a.completionFor(h.a.focus)
			key := completionKeyForLabel(cs, "abi")
			waitForAppCondition(t, h, func() bool { return cs.resolveAttempted[key] })
			if !cs.visible || !h.tt.HasText("abi") {
				t.Fatalf("resolve %s hid usable suggestions: %q", test.name, h.tt.Texts())
			}
			if test.name == "unsupported" && countCalls(h.fc.calls(), "completionItem/resolve") != 0 {
				t.Fatal("unsupported resolve capability reached the client")
			}
		})
	}
}

func waitForResolveStart(t *testing.T, h *harness, started <-chan string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.tt.Frame()
		drainHarnessPosts(h)
		select {
		case label := <-started:
			return label
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("selected completion resolve did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForResolveReturn(t *testing.T, h *harness, returned <-chan struct{}) {
	t.Helper()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("completion resolve worker did not return")
	}
	ran := h.ran
	waitForAppCondition(t, h, func() bool { return h.ran > ran })
}

func waitForAppCondition(t *testing.T, h *harness, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatalf("app condition was not reached; calls=%v", h.fc.calls())
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.tt.Frame()
}

func completionKeyForLabel(cs *completionState, label string) editor.Completion {
	for item := range cs.originals {
		if item.Label == label {
			return item
		}
	}
	return editor.Completion{}
}
