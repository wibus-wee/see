package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"see/internal/hexview"
	"see/internal/rangeio"
	"see/locales"
)

const maxHexCopy = 1 << 20

type hexControl struct {
	anchor, cursor       int64
	selected             bool
	jump, query, message string
	searching            bool
	cancel               context.CancelFunc
	seq                  uint64
	version              int
}

func (s *hexControl) selectByte(pos int64, extend bool) {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.seq++
		s.searching = false
		s.message = ""
	}
	if !extend || !s.selected {
		s.anchor = pos
	}
	s.cursor, s.selected = pos, true
}
func (s *hexControl) bounds() (int64, int64) {
	return min(s.anchor, s.cursor), max(s.anchor, s.cursor) + 1
}
func (s *hexControl) move(delta, size int64, extend bool, list *ui.ListState) {
	if size <= 0 {
		return
	}
	pos := int64(0)
	if s.selected {
		pos = s.cursor + delta
	}
	pos = max(0, min(size-1, pos))
	s.selectByte(pos, extend)
	list.ScrollIntoView(int(pos / hexview.RowBytes))
}
func (a *App) jumpHex(s *hexControl, list *ui.ListState, size int64) {
	text := strings.TrimSpace(s.jump)
	text = strings.TrimPrefix(strings.TrimPrefix(text, "0x"), "0X")
	n, err := strconv.ParseUint(text, 16, 63)
	if err != nil || n >= uint64(size) {
		s.message = "Enter a hexadecimal offset within the file."
		return
	}
	s.selectByte(int64(n), false)
	s.message = ""
	list.ScrollTo(int(n/hexview.RowBytes), ui.Center)
}
func (a *App) findHex(d *doc, s *hexControl, list *ui.ListState) {
	pattern, err := hexview.Pattern(s.query)
	if err != nil || len(pattern) == 0 || len(pattern) > hexview.PageBytes {
		s.message = "Enter text or hex: followed by pairs of hex digits."
		return
	}
	if d.hex == nil || d.hex.sess == nil {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	start := int64(0)
	if s.selected {
		start = min(hexViewSize(d), s.cursor+1)
	}
	ctx, cancel := context.WithCancel(a.wsCtx)
	s.cancel = cancel
	s.seq++
	seq := s.seq
	s.searching, s.message = true, "Searching…"
	path, sess, gen, version := d.path, d.hex.sess, a.gen, d.seq
	go func() {
		defer cancel()
		off, err := hexview.Find(ctx, sess, start, pattern)
		a.post.Post(func() {
			if a.gen != gen || a.docs[path] != d || d.seq != version || s.seq != seq {
				return
			}
			s.cancel = nil
			s.searching = false
			if errors.Is(err, rangeio.ErrChanged) {
				// The scan ran into another version: drop the view built
				// of the old one, so what the user sees next is the file
				// as it is, not a mix.
				d.hex = nil
				return
			}
			if errors.Is(err, context.Canceled) {
				s.message = locales.MsgSearchCanceled()
				return
			}
			if errors.Is(err, hexview.ErrNotFound) {
				s.message = locales.MsgNoMatchAfterTheCurrentByte()
				return
			}
			if err != nil {
				s.message = locales.MsgSearchFailed() + err.Error()
				return
			}
			s.selectByte(off, false)
			s.cursor = off + int64(len(pattern)) - 1
			s.message = locales.Catalog.Format(i18n.Locale(), "msgMatchAtX", map[string]any{"offset": fmt.Sprintf("0x%X", off)})
			list.ScrollTo(int(off/hexview.RowBytes), ui.Center)
		})
	}()
}
func (a *App) copyHex(d *doc, s *hexControl, services ui.Services) {
	if !s.selected {
		return
	}
	start, end := s.bounds()
	if end-start > maxHexCopy {
		s.message = locales.MsgSelectAtMostAMiBToCopy()
		return
	}
	if d.hex == nil || d.hex.sess == nil {
		return
	}
	path, sess, gen, version := d.path, d.hex.sess, a.gen, d.seq
	go func() {
		data, _, err := sess.ReadAt(a.wsCtx, start, end-start)
		a.post.Post(func() {
			if a.gen != gen || a.docs[path] != d || d.seq != version {
				return
			}
			if errors.Is(err, rangeio.ErrChanged) {
				d.hex = nil
				s.message = locales.MsgCouldNotCopyBytes() + err.Error()
				return
			}
			if err != nil {
				s.message = locales.MsgCouldNotCopyBytes() + err.Error()
				return
			}
			encoded := strings.ToUpper(hex.EncodeToString(data))
			var b strings.Builder
			b.Grow(len(encoded) + len(data) - 1)
			for i := 0; i < len(encoded); i += 2 {
				if i > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(encoded[i : i+2])
			}
			services.WriteClipboard(b.String())
			s.message = locales.Catalog.Format(i18n.Locale(), "msgCopiedDBytesAsHex", map[string]any{"count": len(data)})
		})
	}()
}
