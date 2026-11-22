package app

import (
	"context"
	"testing"
	"time"

	"see/internal/lsp"
)

func TestCaretSymbolHighlights(t *testing.T) {
	offsets := scopeOffsets(scopeEditFixture, "名字")
	outer := []int{offsets[0], offsets[1], offsets[4]}
	wantPosition := lspPositionAt(scopeEditFixture, offsets[1], lsp.UTF16)
	h, d, _ := newScopeHarness(t, func(_ context.Context, _ string, pos lsp.Position) ([]lsp.DocumentHighlight, error) {
		if pos != wantPosition {
			t.Errorf("request position = %+v, want %+v", pos, wantPosition)
		}
		ranges := scopeHighlights(scopeEditFixture, outer, "名字", lsp.UTF16)
		ranges[0].Kind = documentHighlightWrite
		return ranges, nil
	})
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	s := &h.a.symbolHighlights
	if s.timer == nil || s.cancel != nil {
		t.Fatal("expected a debounced request")
	}
	h.settle(func() bool { return len(s.backgrounds) == len(outer) })
	for i, bg := range s.backgrounds {
		if bg.Start != outer[i] || bg.End != outer[i]+len("名字") {
			t.Fatalf("background %d = %+v", i, bg)
		}
	}
	if write, read := s.backgrounds[0].Color.A, s.backgrounds[1].Color.A; read < 51 || write <= read {
		t.Fatalf("occurrence tint alpha: write = %d, read = %d; want read at least 20%% and write stronger", write, read)
	}
	st.Select(offsets[1], offsets[1]+len("名字"))
	h.tt.Frame()
	if s.target.editor != nil || len(s.backgrounds) != 0 {
		t.Fatal("semantic highlighting remained over a lexical selection")
	}
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	h.settle(func() bool { return len(s.backgrounds) == len(outer) })
	st.GoTo(d.buf.Pos(0))
	h.a.cfg.HideSymbolHighlights = true
	h.tt.Frame()
	if s.target.editor != nil || s.timer != nil || len(s.backgrounds) != 0 {
		t.Fatal("disabling the setting did not clear highlighting")
	}
}

func TestCaretSymbolHighlightsDropStaleReply(t *testing.T) {
	offsets := scopeOffsets(scopeEditFixture, "名字")
	started, release := make(chan struct{}), make(chan struct{})
	h, d, _ := newScopeHarness(t, func(ctx context.Context, _ string, _ lsp.Position) ([]lsp.DocumentHighlight, error) {
		close(started)
		select {
		case <-ctx.Done():
		case <-release:
		}
		return scopeHighlights(scopeEditFixture, offsets, "名字", lsp.UTF16), nil
	})
	defer close(release)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	h.settle(func() bool { return h.a.symbolHighlights.cancel != nil })
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("document highlight request did not start")
	}
	st.GoTo(d.buf.Pos(len("package demo\n")))
	h.tt.Frame()
	s := &h.a.symbolHighlights
	if s.cancel != nil || len(s.backgrounds) != 0 {
		t.Fatal("moving off the symbol did not cancel and clear highlighting")
	}
	h.settle(func() bool { return s.timer == nil })
	if len(s.backgrounds) != 0 {
		t.Fatal("stale highlights became visible after moving off the symbol")
	}
}
