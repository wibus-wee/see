package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
)

const (
	symbolHighlightDelay   = 180 * time.Millisecond
	documentHighlightWrite = 3
)

type symbolHighlightTarget struct {
	doc     *doc
	editor  *editor.State
	server  *langserver.Server
	version uint64
	caret   int
}

type symbolHighlightState struct {
	target      symbolHighlightTarget
	timer       *time.Timer
	cancel      context.CancelFunc
	seq         uint64
	backgrounds []editor.TextBackground
}

func (a *App) symbolHighlightTarget() symbolHighlightTarget {
	if a.cfg.HideSymbolHighlights || !a.workspaceTrusted() {
		return symbolHighlightTarget{}
	}
	d, st := a.scopeEditDocument()
	if st == nil || a.scopePendingState == st {
		return symbolHighlightTarget{}
	}
	sels := st.Selections()
	if len(sels) != 1 || sels[0].Anchor != sels[0].Head {
		return symbolHighlightTarget{}
	}
	if m := a.serverFor(d.path); m != nil {
		return symbolHighlightTarget{d, st, m, d.buf.Version(), sels[0].Head}
	}
	return symbolHighlightTarget{}
}

func (a *App) syncSymbolHighlights() {
	s := &a.symbolHighlights
	target := a.symbolHighlightTarget()
	if target == s.target {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.target, s.backgrounds = target, nil
	s.seq++
	if target.doc == nil {
		return
	}
	seq, gen := s.seq, a.gen
	s.timer = time.AfterFunc(symbolHighlightDelay, func() {
		a.post.Post(func() {
			if s.seq == seq && a.gen == gen && a.symbolHighlightTarget() == target {
				s.timer = nil
				a.requestSymbolHighlights(target, seq, gen)
			}
		})
	})
}

func (a *App) requestSymbolHighlights(target symbolHighlightTarget, seq uint64, gen int) {
	m := target.server
	if state, _ := m.Status(); state == langserver.Failed {
		return
	}
	if c := m.Current(); c != nil {
		if p, ok := c.(documentHighlightsProvider); !ok || !p.HasDocumentHighlights() {
			return
		}
	}
	d := target.doc
	snap, uri := d.buf.Snapshot(), d.uri
	_, offset, ok := scopeIdentifierAt(snap, target.caret)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	a.symbolHighlights.cancel = cancel
	go func() {
		defer cancel()
		c, err := m.Get(ctx)
		if err != nil {
			return
		}
		p, ok := c.(documentHighlightsProvider)
		if !ok || !p.HasDocumentHighlights() || m.Sync(ctx, c, uri, snap) != nil {
			return
		}
		enc := c.Encoding()
		highlights, err := p.DocumentHighlights(ctx, uri, scopeLSPPosition(snap, offset, enc))
		if err != nil {
			return
		}
		a.post.Post(func() {
			s := &a.symbolHighlights
			if a.gen != gen || s.seq != seq || a.symbolHighlightTarget() != target {
				return
			}
			s.cancel = nil
			for _, h := range highlights {
				start, startOK := scopeOffset(snap, h.Range.Start, enc)
				end, endOK := scopeOffset(snap, h.Range.End, enc)
				if !startOK || !endOK || start >= end {
					continue
				}
				alpha := float32(0.23)
				if h.Kind == documentHighlightWrite {
					alpha = 0.36
				}
				s.backgrounds = append(s.backgrounds, editor.TextBackground{
					Start: start, End: end, Color: ui.RGBA(0x58, 0x87, 0xbb, alpha),
				})
			}
		})
	}()
}
