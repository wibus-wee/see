package app

import (
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

type structuralHighlighter struct{ editor.Highlighter }

func (structuralHighlighter) FoldEnd(line int) (int, bool) { return line + 2, true }
func (structuralHighlighter) Enclosing(start, end int) (editor.Range, bool) {
	return editor.Range{Start: start - 1, End: end + 1}, true
}
func (structuralHighlighter) EnclosingBrackets(start, end int) (editor.Range, editor.Range, bool) {
	return editor.Range{Start: start - 1, End: start}, editor.Range{Start: end, End: end + 1}, true
}
func (structuralHighlighter) IndentAfter(int) (int, bool) { return 2, true }

func TestInlayTrackingPreservesSyntaxCapabilities(t *testing.T) {
	base := structuralHighlighter{}
	tracked := trackInlayEdits(base, &doc{})
	if got, ok := tracked.(editor.Folder).FoldEnd(3); !ok || got != 5 {
		t.Fatalf("lost folding: %d %v", got, ok)
	}
	if got, ok := tracked.(editor.SyntaxSelector).Enclosing(3, 5); !ok || got != (editor.Range{Start: 2, End: 6}) {
		t.Fatalf("lost syntax selection: %+v %v", got, ok)
	}
	if open, close, ok := tracked.(editor.BracketMatcher).EnclosingBrackets(3, 5); !ok || open.End != 3 || close.Start != 5 {
		t.Fatalf("lost brackets: %+v %+v %v", open, close, ok)
	}
	if got, ok := tracked.(editor.Indenter).IndentAfter(3); !ok || got != 2 {
		t.Fatalf("lost indentation: %d %v", got, ok)
	}

	// Merely implementing these interfaces disables mygo's lexical fallbacks.
	plain := trackInlayEdits(struct{ editor.Highlighter }{}, &doc{})
	if _, ok := plain.(editor.Folder); ok {
		t.Fatal("tracking suppressed default folding")
	}
	if _, ok := plain.(editor.BracketMatcher); ok {
		t.Fatal("tracking suppressed default bracket matching")
	}
}
