package lsp

import (
	"sort"

	"github.com/egoist/mygo/ui/editor"
)

// ApplyTextEdits turns LSP text edits into buffer edits on snap, the
// text they are of: each Range's positions are columns in the encoding
// the client negotiated, and the edits it gives apply to the buffer in
// one step, all against the same original text. Edits a document's own
// ranges overlap, as a whole-line replacement over a word's, are cut to
// follow the first, as Buffer.Edit does.
func ApplyTextEdits(snap editor.Snapshot, edits []TextEdit, encoding string) []editor.Edit {
	out := make([]editor.Edit, 0, len(edits))
	for _, e := range edits {
		start, end := textEditOffsets(snap, e.Range, encoding)
		if start > end {
			continue // a range past the document's end is the server's, not the text's
		}
		out = append(out, editor.Edit{Start: start, End: end, Text: e.NewText})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	// Buffer.Edit wants disjoint, sorted edits: an edit whose start falls
	// inside the previous is cut to its end, as the editor cuts overlaps.
	for i := 1; i < len(out); i++ {
		if out[i].Start < out[i-1].End {
			out[i].Start = out[i-1].End
		}
	}
	return out
}

// textEditOffsets maps a Range's lines and characters to byte offsets
// of snap's text, the end first so a bad end cannot push start past it.
// A position past a line's end lands at the line's end.
func textEditOffsets(snap editor.Snapshot, r Range, encoding string) (int, int) {
	startLine, startCol := clampPosition(snap, r.Start, encoding)
	endLine, endCol := clampPosition(snap, r.End, encoding)
	start := snap.LineStart(startLine) + startCol
	end := snap.LineStart(endLine) + endCol
	if end < start {
		return end, end
	}
	return start, end
}

func clampPosition(snap editor.Snapshot, p Position, encoding string) (line, col int) {
	line = p.Line
	if line >= snap.LineCount() {
		line = snap.LineCount() - 1
	}
	if line < 0 {
		line = 0
	}
	text := snap.Line(line)
	return line, ColumnFromLSP(text, p.Character, encoding)
}
