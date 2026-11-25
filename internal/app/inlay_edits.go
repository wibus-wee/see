package app

import "github.com/egoist/mygo/ui/editor"

// inlayHighlighter follows the buffer's actual edits, including undo and remote
// edits. The document owns the offsets so split views never move them twice.
type inlayHighlighter struct {
	editor.Highlighter
	doc *doc
}

func trackInlayEdits(base editor.Highlighter, d *doc) editor.Highlighter {
	h := &inlayHighlighter{Highlighter: base, doc: d}
	// Folder and BracketMatcher suppress editor fallbacks even when they return
	// false. Preserve their presence, not just their successful answers.
	f, folds := base.(editor.Folder)
	b, brackets := base.(editor.BracketMatcher)
	switch {
	case folds && brackets:
		return struct {
			*inlayHighlighter
			editor.Folder
			editor.BracketMatcher
		}{h, f, b}
	case folds:
		return struct {
			*inlayHighlighter
			editor.Folder
		}{h, f}
	case brackets:
		return struct {
			*inlayHighlighter
			editor.BracketMatcher
		}{h, b}
	default:
		return h
	}
}

func (h *inlayHighlighter) Edit(old, next editor.Snapshot, changes []editor.Change) {
	h.Highlighter.Edit(old, next, changes)
	d := h.doc
	if d.inlayVersion != old.Version() {
		return // The cache may already contain a reply for a newer snapshot.
	}
	for _, lane := range [][]editor.Inlay{d.languageInlays, d.parameterInlays} {
		for i := range lane {
			at := lane[i].Offset
			// Changes share the old coordinate space; use the last one before at.
			for j := len(changes) - 1; j >= 0; j-- {
				c := changes[j]
				if at < c.OldStart {
					continue
				}
				if at >= c.OldEnd {
					at += c.NewEnd - c.OldEnd
				} else {
					at = c.NewEnd
				}
				break
			}
			lane[i].Offset = at
		}
	}
	d.inlayVersion = next.Version()
}

func (h *inlayHighlighter) Enclosing(start, end int) (editor.Range, bool) {
	if s, ok := h.Highlighter.(editor.SyntaxSelector); ok {
		return s.Enclosing(start, end)
	}
	return editor.Range{}, false
}

func (h *inlayHighlighter) IndentAfter(offset int) (int, bool) {
	if i, ok := h.Highlighter.(editor.Indenter); ok {
		return i.IndentAfter(offset)
	}
	return 0, false
}
