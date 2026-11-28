package app

import (
	"errors"
	"fmt"
	"math"
	"see/locales"

	"github.com/egoist/mygo/ui"
	"see/internal/hexview"
	"see/internal/rangeio"
	"see/internal/workspace"
)

const hexCachePages = 8

type hexPage struct {
	data []byte
	used uint64
}

// hexViewSize is the size the hex view works with: the pinned version's
// when a session exists, the document's metadata otherwise.
func hexViewSize(d *doc) int64 {
	if d.hex != nil && d.hex.sess != nil {
		return d.hex.sess.Size()
	}
	return d.size
}

// hexState is one view of one version of the file: every page and every
// scan goes through the session, so the view never mixes bytes of two
// versions. A file change fails the reads, the state is dropped, and the
// next frame pins the version then on disk.
type hexState struct {
	sess    *rangeio.Session
	pages   map[int64]*hexPage
	pending map[int64]bool
	clock   uint64
	err     error
}

func (a *App) canViewBytes(path string) bool {
	d := a.docs[path]
	return d != nil && !d.untitled && !d.loading && d.err == nil && !(d.kind == workspace.Binary && d.image == nil && !d.video)
}

func (a *App) hexView(c *ui.Context, g *group, d *doc) {
	if d.hex == nil {
		h := &hexState{pages: map[int64]*hexPage{}, pending: map[int64]bool{}}
		sess, err := rangeio.Open(d.path)
		if err != nil {
			h.err = err
		} else {
			h.sess = sess
		}
		d.hex = h
	}
	h := d.hex
	if g.hexLists == nil {
		g.hexLists = map[string]*ui.ListState{}
	}
	if g.hexLists[d.path] == nil {
		g.hexLists[d.path] = &ui.ListState{}
	}
	state := g.hexLists[d.path]
	if g.hexControls == nil {
		g.hexControls = map[string]*hexControl{}
	}
	if g.hexControls[d.path] == nil {
		g.hexControls[d.path] = &hexControl{}
	}
	control := g.hexControls[d.path]
	if control.version != d.seq {
		if control.cancel != nil {
			control.cancel()
			control.cancel = nil
		}
		control.seq++
		control.version = d.seq
		control.searching = false
		control.selected = false
		control.message = ""
	}
	services := c.Services()
	for _, item := range []struct {
		key   ui.Key
		delta int64
	}{{ui.KeyLeft, -1}, {ui.KeyRight, 1}, {ui.KeyUp, -16}, {ui.KeyDown, 16}} {
		item := item
		state.Handle.OnShortcut(c, 0, item.key, func() { control.move(item.delta, hexViewSize(d), false, state) })
		state.Handle.OnShortcut(c, ui.Shift, item.key, func() { control.move(item.delta, hexViewSize(d), true, state) })
	}
	state.Handle.OnShortcut(c, ui.Cmd, ui.KeyC, func() { a.copyHex(d, control, services) })
	if a.focusGroupTarget == g {
		state.Handle.Focus(c)
	}
	// Rows, jumps and selection bounds all follow the pinned version, not
	// the document's metadata, so the view is consistent within itself
	// even while the document is a reload behind.
	viewSize := d.size
	if h.sess != nil {
		viewSize = h.sess.Size()
	}
	count := hexview.Rows(viewSize)
	if count > int64(math.MaxInt) {
		count = int64(math.MaxInt)
	}
	needed := map[int64]bool{}
	charWidth, _ := c.MeasureText(0, ui.Span{Text: "0", Font: "monospace", Size: 12})
	digits := hexOffsetDigits(viewSize)
	offsetWidth := float32(digits+3) * charWidth
	hexWidth := float32(49) * charWidth
	rowWidth := 32 + offsetWidth + hexWidth + 16*charWidth
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		ui.Row(c).FillWidth().Padding(8, 14).Gap(7).AlignItems(ui.Center).Background(colors(c).Toolbar).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Hairline).Children(func() {
			ui.Text(c, locales.MsgHEXVIEW()).FontSize(10).Bold().TextColor(colors(c).Muted).Shrink(0)
			ui.Box(c).Width(1).Height(18).Background(colors(c).Separator).Shrink(0)
			jump := ui.TextInput(c, &control.jump).Label(locales.MsgHexOffset()).Placeholder(locales.MsgOffset()).Width(105).Height(26).FontSize(12)
			if jump.Submitted() {
				a.jumpHex(control, state, viewSize)
			}
			if ui.Button(c, locales.MsgGo()).Height(26).Clicked() {
				a.jumpHex(control, state, viewSize)
			}
			ui.Box(c).Width(1).Height(18).Background(colors(c).Separator).Shrink(0)
			search := ui.TextInput(c, &control.query).Label(locales.MsgFindBytes()).Placeholder(locales.MsgFindTextOrHex00FF()).Grow(1).MinWidth(80).Height(26).FontSize(12)
			if search.Changed() && control.searching {
				control.cancel()
				control.cancel = nil
				control.seq++
				control.searching = false
				control.message = ""
			}
			if search.Submitted() {
				a.findHex(d, control, state)
			}
			if ui.Button(c, locales.MsgFindNext()).Height(26).Disabled(control.searching).Clicked() {
				a.findHex(d, control, state)
			}
			if control.searching && ui.Button(c, locales.MsgCancelSearch()).Height(26).Clicked() && control.cancel != nil {
				control.cancel()
			}
			if ui.Button(c, locales.MsgCopyHex()).Height(26).Disabled(!control.selected).Clicked() {
				a.copyHex(d, control, services)
			}
		})
		head := ui.Row(c).FillWidth().Clip().Height(30).AlignItems(ui.Center).Background(colors(c).TabBar).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Hairline)
		head.Children(func() {
			ui.Row(c).Width(rowWidth).Shrink(0).Padding(0, 16).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "OFFSET").Width(offsetWidth).Font("monospace").FontSize(10).TextColor(colors(c).Muted).Shrink(0)
				for i := 0; i < hexview.RowBytes; i++ {
					if i == 8 {
						ui.Box(c).Width(charWidth).Shrink(0)
					}
					ui.Text(c, fmt.Sprintf("%02X", i)).Width(charWidth * 3).Font("monospace").FontSize(10).TextColor(colors(c).Muted).Shrink(0)
				}
				ui.Text(c, "ASCII").Width(16 * charWidth).Font("monospace").FontSize(10).TextColor(colors(c).Muted).Shrink(0)
			})
		})
		if h.err != nil {
			ui.Row(c).Gap(8).Padding(12, 16).Children(func() {
				ui.Text(c, locales.MsgCouldNotReadFileBytes()+h.err.Error()).FontSize(12).TextColor(colors(c).Err)
				if ui.Button(c, locales.Retry()).Clicked() {
					d.hex = nil
				}
			})
		} else if count == 0 {
			ui.Text(c, locales.MsgEmptyFile()).Padding(12, 16)
		} else {
			list := ui.List(c.Key(d.path), state, int(count)).Grow(1).FillWidth().RowWidth(rowWidth).Label(locales.MsgFileBytes())
			list.Rows(func(row ui.ListRow) {
				offset := int64(row.Index) * hexview.RowBytes
				pageOffset := offset / hexview.PageBytes * hexview.PageBytes
				if page := h.pages[pageOffset]; page != nil {
					h.clock++
					page.used = h.clock
					start := int(offset - pageOffset)
					end := min(start+hexview.RowBytes, len(page.data))
					if start < end {
						a.hexRow(c, state, control, offset, page.data[start:end], digits, charWidth, offsetWidth)
						return
					}
				}
				needed[pageOffset] = true
				ui.Text(c, fmt.Sprintf("%0*X  …", digits, offset)).Font("monospace").FontSize(12).SingleLine().Padding(3, 16)
			})
			head.ScrollWith(list)
		}
		ui.Row(c).FillWidth().Padding(6, 16).Gap(12).AlignItems(ui.Center).Background(colors(c).TabBar).BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Hairline).Children(func() {
			ui.Text(c, locales.MsgREADONLY()).FontSize(10).Bold().TextColor(colors(c).Muted)
			ui.Text(c, size(viewSize)).FontSize(11).TextColor(colors(c).Muted)
			if control.selected {
				lo, hi := control.bounds()
				ui.Text(c, fmt.Sprintf("0x%X – 0x%X  ·  %s", lo, hi-1, hexSelectionSize(hi-lo))).Font("monospace").FontSize(11).TextColor(colors(c).Text)
			}
			ui.Box(c).Grow(1)
			if control.message != "" {
				ui.Text(c, control.message).FontSize(11).TextColor(colors(c).Muted).SingleLine().Shrink(1)
			}
		})
	})

	for offset := range needed {
		a.requestHexPage(d, h, offset)
	}
}

func (a *App) requestHexPage(d *doc, h *hexState, offset int64) {
	if h.err != nil || h.sess == nil || h.pending[offset] || len(h.pending) >= hexCachePages {
		return
	}
	h.pending[offset] = true
	path, sess, gen, seq, ctx := d.path, h.sess, a.gen, d.seq, a.wsCtx
	go func() {
		data, _, err := sess.ReadAt(ctx, offset, hexview.PageLen(offset, sess.Size()))
		a.post.Post(func() {
			// The read is over whatever the completion finds: pending
			// must clear even for a dropped result, or the page is
			// never asked for again. A refresh of the unchanged file
			// moves d.seq without resetting d.hex (the reload takes
			// loadDoc's stamp shortcut), so guarding the delete on the
			// state being current would wedge the page forever.
			delete(h.pending, offset)
			if a.gen != gen || a.docs[path] != d || d.seq != seq || d.hex != h {
				return
			}
			if errors.Is(err, rangeio.ErrChanged) {
				// The pinned version is gone, so the view built of it
				// is dropped entirely and the next frame pins the
				// version then on disk.
				d.hex = nil
				return
			}
			if err != nil {
				h.err = err
				return
			}
			if len(h.pages) >= hexCachePages {
				var oldest int64
				age := uint64(math.MaxUint64)
				for key, page := range h.pages {
					if page.used < age {
						oldest, age = key, page.used
					}
				}
				delete(h.pages, oldest)
			}
			h.clock++
			h.pages[offset] = &hexPage{data: data, used: h.clock}
		})
	}()
}

func (a *App) hexRow(c *ui.Context, list *ui.ListState, s *hexControl, offset int64, data []byte, digits int, charWidth, offsetWidth float32) {
	ui.Row(c).Padding(3, 16).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("%0*X", digits, offset)).Width(offsetWidth).Font("monospace").FontSize(12).TextColor(colors(c).Muted).SingleLine()
		for i := 0; i < hexview.RowBytes; i++ {
			if i == 8 {
				ui.Box(c).Width(charWidth).Shrink(0)
			}
			if i >= len(data) {
				ui.Box(c).Width(charWidth * 3).Shrink(0)
				continue
			}
			hexCell(c, list, s, offset+int64(i), fmt.Sprintf("%02X", data[i]), data[i], charWidth*3)
		}
		for i, b := range data {
			ch := byte('.')
			if b >= 32 && b <= 126 {
				ch = b
			}
			hexCell(c, list, s, offset+int64(i), string(ch), b, charWidth)
		}
	})
}

func hexCell(c *ui.Context, list *ui.ListState, s *hexControl, pos int64, text string, value byte, width float32) {
	cell := ui.ButtonBase(c.Key(fmt.Sprintf("%d:%s", pos, text))).Label(fmt.Sprintf(locales.MsgByteXS(), pos, text)).Width(width).Shrink(0).Padding(0).FocusRing(false)
	if s.selected {
		lo, hi := s.bounds()
		if pos >= lo && pos < hi {
			cell.Background(c.Theme().Accent.Alpha(0.25))
		}
	}
	cell.Children(func() {
		t := ui.Text(c, text).Font("monospace").FontSize(12).SingleLine()
		if value == 0 {
			t.TextColor(c.Theme().TextMuted)
		}
	})
	if cell.Clicked() {
		s.selectByte(pos, cell.ClickModifiers()&ui.Shift != 0)
		list.Handle.Focus(c)
	}
}

func hexOffsetDigits(size int64) int {
	if size <= 0 {
		return 8
	}
	digits := len(fmt.Sprintf("%X", size-1))
	return max(8, digits)
}

func hexSelectionSize(n int64) string {
	if n == 1 {
		return "1 byte"
	}
	return fmt.Sprintf("%d bytes", n)
}
