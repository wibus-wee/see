package app

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
)

// semanticTokensClient is a language client that answers semantic
// tokens requests and names its token stream.
type semanticTokensClient interface {
	SemanticTokensFull() bool
	HasSemanticTokens() bool
	SemanticTokens(ctx context.Context, uri string, first, last int) (*lsp.SemanticTokensData, error)
	SemanticTokenTypes() []string
}

// A semanticToken is one token of the server's stream, decoded: a byte
// span of a line of text and the kind the line's tokens show it in.
type semanticToken struct {
	start, end int // byte offsets within the line
	kind       editor.TokenKind
}

// tokenKindOf is the editor kind an LSP semantic token type shows in.
// The kinds the editor has are fewer than the types servers name; each
// maps to the closest, so a type the legend adds still colors.
func tokenKindOf(lspType string) editor.TokenKind {
	switch lspType {
	case "keyword", "modifier":
		return editor.TokenKeyword
	case "type", "class", "enum", "interface", "struct", "typeParameter":
		return editor.TokenType
	case "function", "method", "macro", "decorator":
		return editor.TokenFunction
	case "property", "event", "enumMember", "label":
		return editor.TokenProperty
	case "number":
		return editor.TokenNumber
	case "string", "regexp":
		return editor.TokenString
	case "comment":
		return editor.TokenComment
	case "operator":
		return editor.TokenOperator
	case "variable", "parameter", "namespace":
		return editor.TokenVariable
	}
	return editor.TokenInvalid
}

// decodeSemanticTokens turns the server's delta-encoded stream into
// tokens over snap's text: five uint32s a token — deltaLine, deltaChar
// (from the previous token's start when on its line, absolute
// otherwise), length, tokenType and tokenModifiers — positions in enc.
// Tokens out of the legend or the text are dropped, and so are
// zero-length ones.
func decodeSemanticTokens(data []uint32, legend []string, snap editor.Snapshot, enc string) map[int][]semanticToken {
	out := map[int][]semanticToken{}
	line, col := 0, 0
	for i := 0; i+5 <= len(data); i += 5 {
		dLine, dChar, length, typ := data[i], data[i+1], data[i+2], data[i+3]
		line += int(dLine)
		if dLine != 0 {
			col = 0
		}
		col += int(dChar)
		if line < 0 || line >= snap.LineCount() || int(typ) >= len(legend) {
			continue
		}
		kind := tokenKindOf(legend[typ])
		if kind == editor.TokenInvalid || length == 0 {
			continue
		}
		text := snap.Line(line)
		start := lsp.ColumnFromLSP(text, col, enc)
		end := lsp.ColumnFromLSP(text, col+int(length), enc)
		if end <= start {
			continue
		}
		out[line] = append(out[line], semanticToken{start: start, end: end, kind: kind})
	}
	return out
}

// semanticHighlighter is a buffer's highlighter over the language's
// tokenizer: lines' tokens are the tokenizer's until the server
// answers, then the semantic tokens merge in over them, line by line.
// The fetch runs on a snapshot of the text, off the main thread; an
// edit makes the tokens stale — lines go on showing the tokenizer's
// colors until a fetch of the new text lands.
// semanticHighlighterHook observes each highlighter the factory
// makes; tests set it to reach the live instance. Nil in the app.
var semanticHighlighterHook func(*semanticHighlighter)

type semanticHighlighter struct {
	base       editor.Highlighter
	app        *App
	doc        *doc
	server     *langserver.Server
	gen        int
	uri        string
	ctx        context.Context
	invalidate func()

	lineOf   map[int][]semanticToken // the tokens the server gave, by line
	version  uint64                  // the buffer version they are of
	ready    bool                    // lineOf is filled, for Tokens to merge
	stale    atomic.Bool             // the text moved past what lineOf colors
	fetching atomic.Bool             // one fetch at a time
}

// Edit follows the buffer's edits: the base tokenizer does, and the
// semantic tokens of the text they were decoded against are stale, so
// lines they colored show the base's until a new fetch answers.
func (h *semanticHighlighter) Edit(old, new editor.Snapshot, changes []editor.Change) {
	h.base.Edit(old, new, changes)
	if new.Version() != h.version {
		h.stale.Store(true)
		h.wake(new)
	}
}

// Highlight works the base's part; the semantic layer asks for more
// tokens when it is stale, never inside the frame's budget.
func (h *semanticHighlighter) Highlight(last int, budget time.Duration) bool {
	return h.base.Highlight(last, budget)
}

// Tokens appends the line's tokens: the semantic ones over the base's
// once they are of this text, else the base's alone.
func (h *semanticHighlighter) Tokens(i int, text string, dst []editor.Token) ([]editor.Token, bool) {
	base, ok := h.base.Tokens(i, text, nil)
	sems := h.lineOf[i]
	if !h.ready || len(sems) == 0 {
		return append(dst, base...), ok
	}
	return append(dst, mergeTokens(base, sems)...), ok
}

// mergeTokens lays the semantic tokens sems over base's: where they
// cover a span it takes their kind, and the gaps between them keep
// base's. sems are the line's semantic tokens in order, their offsets
// bytes within the line, as base's Ends are.
func mergeTokens(base []editor.Token, sems []semanticToken) []editor.Token {
	var merged []editor.Token
	pos, k := 0, 0
	emit := func(end int, kind editor.TokenKind) {
		if end <= pos {
			return
		}
		if last := len(merged) - 1; last >= 0 && merged[last].End == pos && merged[last].Kind == kind {
			merged[last].End = end
		} else {
			merged = append(merged, editor.Token{End: end, Kind: kind})
		}
		pos = end
	}
	for _, bt := range base {
		// Semantic tokens wholly before this token's span are done;
		// those reaching past its end keep coloring the next spans.
		for k < len(sems) && sems[k].end <= pos {
			k++
		}
		for j := k; j < len(sems) && sems[j].start < bt.End; j++ {
			s := sems[j]
			if s.start > pos {
				emit(s.start, bt.Kind)
			}
			emit(min(s.end, bt.End), s.kind)
			if s.end <= bt.End {
				k = j + 1
			}
		}
		emit(bt.End, bt.Kind)
	}
	return merged
}

// wake starts a fetch of snap's tokens when none is running.
func (h *semanticHighlighter) wake(snap editor.Snapshot) {
	if h.fetching.CompareAndSwap(false, true) {
		go h.fetch(snap)
	}
}

// fetch asks the server for the document's tokens, on the goroutine
// the request is on; the answer is applied only while the buffer still
// is the text it was decoded against and the doc is still the app's.
func (h *semanticHighlighter) fetch(snap editor.Snapshot) {
	defer h.fetching.Store(false)
	c, err := h.server.Get(h.ctx)
	if err != nil {
		return
	}
	sc, ok := c.(semanticTokensClient)
	if !ok || !sc.HasSemanticTokens() {
		return
	}
	if err := h.server.Sync(h.ctx, c, h.uri, snap); err != nil {
		return
	}
	first, last := -1, -1
	if !sc.SemanticTokensFull() {
		first, last = 0, snap.LineCount()-1
	}
	data, err := sc.SemanticTokens(h.ctx, h.uri, first, last)
	if err != nil {
		return
	}
	lineOf := decodeSemanticTokens(data.Data, sc.SemanticTokenTypes(), snap, c.Encoding())
	h.app.post.Post(func() {
		if h.app.gen != h.gen || h.app.docs[h.doc.path] != h.doc ||
			h.doc.buf == nil || h.doc.buf.Version() != snap.Version() {
			return
		}
		h.lineOf, h.version = lineOf, snap.Version()
		h.stale.Store(false)
		h.ready = true
		h.invalidate()
	})
}
