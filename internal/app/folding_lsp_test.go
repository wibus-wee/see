package app

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

// foldSource is a file the indentation fallback folds at lines 7 and 11;
// a server saying only lines 7-12 fold — and that 11 folds to 13, which
// the guess would not say — tells who the gutter is listening to.
const foldLSPSource = "package example\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc first() {\n\tfmt.Println(os.Args)\n}\n\nfunc second() {\n\tfirst()\n}\n"

// foldHarness opens a Go file whose fake server answers folding ranges.
func foldHarness(t *testing.T, ranges func(context.Context, string) ([]lsp.FoldingRange, error)) (*harness, *doc) {
	t.Helper()
	root := writeTree(t, map[string]string{"main.go": foldLSPSource})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.foldingRanges = ranges
	d := h.open("main.go")
	return h, d
}

func settleFolds(h *harness, d *doc) {
	h.settle(func() bool {
		h.tt.Frame()
		return len(d.foldRanges) > 0 && d.foldFolder != nil
	})
}

func TestFoldingRangesBecomeTheGutter(t *testing.T) {
	h, d := foldHarness(t, func(context.Context, string) ([]lsp.FoldingRange, error) {
		return []lsp.FoldingRange{{StartLine: 7, EndLine: 12, Kind: "region"}}, nil
	})
	settleFolds(h, d)
	st := h.a.activeEditor()
	// Line 7 folds to 12 as the server said; the indentation guess would
	// have ended at 9, before the blank line second() follows.
	if end, ok := d.foldFolder.FoldEnd(7); !ok || end != 12 {
		t.Fatalf("FoldEnd(7) = %d, %v; want 12, true", end, ok)
	}
	st.Fold(7)
	if got := st.Folded(); !slices.Equal(got, []int{7}) {
		t.Fatalf("Fold(7): %v", got)
	}
	// Line 11, foldable by indentation, is not a range's start: the
	// server's word stands, and Fold leaves it alone.
	st.Run(editor.UnfoldAll)
	st.Fold(11)
	if got := st.Folded(); len(got) != 0 {
		t.Fatalf("Fold(11) folded a line the server says starts nothing: %v", got)
	}
}

func TestFoldingRangesUnservedAsksNothing(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": foldLSPSource})
	h := newHarness(t, root) // no trust granted: the server must not be asked
	h.fc.foldingRanges = func(context.Context, string) ([]lsp.FoldingRange, error) {
		return []lsp.FoldingRange{{StartLine: 7, EndLine: 9}}, nil
	}
	h.open("main.go")
	h.tt.Frame()
	drainHarnessPosts(h)
	if countCalls(h.fc.calls(), "foldingRange") != 0 {
		t.Fatal("an untrusted workspace asked its server for folding ranges")
	}
}

func TestFoldingRangesUnsupportedKeepsFallback(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": foldLSPSource})
	h := newHarness(t, root)
	h.a.cfg.Trust(root) // fc.foldingRanges nil: the server answers none
	d := h.open("main.go")
	h.tt.Frame()
	drainHarnessPosts(h)
	if d.foldFolder != nil {
		t.Fatal("the language was wrapped for a server answering no ranges")
	}
	st := h.a.activeEditor()
	st.Fold(7) // the indentation fallback folds the func
	if got := st.Folded(); !slices.Equal(got, []int{7}) {
		t.Fatalf("indentation fallback lost: %v", got)
	}
}

func TestFoldingRangesErrorKeepsFallback(t *testing.T) {
	h, d := foldHarness(t, func(context.Context, string) ([]lsp.FoldingRange, error) {
		return nil, errors.New("range boom")
	})
	h.settle(func() bool {
		h.tt.Frame()
		return countCalls(h.fc.calls(), "foldingRange") > 0
	})
	h.tt.Frame()
	if d.foldFolder != nil {
		t.Fatal("a failed request wrapped the language")
	}
	st := h.a.activeEditor()
	st.Fold(7)
	if got := st.Folded(); !slices.Equal(got, []int{7}) {
		t.Fatalf("indentation fallback lost to a failed request: %v", got)
	}
}

func TestFoldingRangesStaleReplyDropped(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h, d := foldHarness(t, func(ctx context.Context, _ string) ([]lsp.FoldingRange, error) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []lsp.FoldingRange{{StartLine: 7, EndLine: 12}}, nil
	})
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-started:
			goto requested
		case fn := <-h.q:
			fn()
		case <-deadline:
			t.Fatal("folding range request never started")
		case <-time.After(10 * time.Millisecond):
			h.tt.Frame()
		}
	}
requested:
	// The buffer moves on before the reply lands: it must not apply.
	d.buf.Edit(editor.Edit{Text: "\n"})
	close(release)
	time.Sleep(50 * time.Millisecond) // the fetch's posts land behind the reply
	drainHarnessPosts(h)
	h.tt.Frame()
	if len(d.foldRanges) != 0 || d.foldFolder != nil {
		t.Fatalf("stale ranges applied: %+v", d.foldRanges)
	}
}
