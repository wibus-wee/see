package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"see/internal/workspace"
)

// quickOpenPanel searches files, or commands when the query starts with >.
func (a *app) quickOpenPanel(c *ui.Context) {
	q := &a.quick
	a.rankQuick()
	if c.Shortcut(0, ui.KeyEscape) {
		q.open = false
		return
	}
	ui.Overlay(c, func() {
		backdrop := ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.18))
		backdrop.OnClick(func() { q.open = false })
		backdrop.Children(func() {
			panel := ui.Column(c).Margin(90, 0, 0, 0).Width(640).MaxHeight(460).Radius(14).Padding(8).Gap(6).
				Material(glass.Glass{}).Border(1, colors(c).popoverEdge).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.5))
			panel.OnClick(func() {}) // clicks on the panel stay on it
			panel.Children(func() {
				ui.Row(c).Padding(4, 8).Gap(8).AlignItems(ui.Center).Children(func() {
					symbolIcon(c, icSearch, 20, colors(c).muted)
					in := ui.TextInput(c, &q.query).Placeholder("Open Quickly").FontSize(20).Grow(1).AutoFocus().Background(ui.Transparent).BorderWidth(0)
					// The field has the keyboard: it takes the list's keys.
					switch {
					case in.Submitted():
						a.chooseQuick(q.sel)
					case in.Shortcut(0, ui.KeyEscape):
						q.open = false
					case in.Shortcut(0, ui.KeyDown):
						q.sel = min(q.sel+1, len(q.results)-1)
						q.list.ScrollTo(q.sel, ui.Center)
					case in.Shortcut(0, ui.KeyUp):
						q.sel = max(q.sel-1, 0)
						q.list.ScrollTo(q.sel, ui.Center)
					}
				})
				commandMode := strings.HasPrefix(q.query, ">")
				symbolMode := strings.HasPrefix(q.query, "#")
				switch {
				case !commandMode && a.root == "":
					ui.Text(c, "Open a folder to search files, or type > to search commands.").TextColor(colors(c).muted).Padding(8)
				case symbolMode && !a.cfg.trusted(a.root):
					if ui.Button(c, "Trust folder to search symbols").Clicked() {
						query := q.query
						q.open = false
						a.trustAsk = true
						a.afterTrust = func() { a.openQuickPanel(query) }
					}
				case symbolMode && strings.TrimSpace(q.query[1:]) == "":
					ui.Text(c, "Type a symbol name to search the workspace.").TextColor(colors(c).muted).Padding(8)
				case symbolMode && q.symbolLoading && len(q.results) == 0:
					ui.Row(c).Padding(8).Gap(8).Children(func() { ui.Spinner(c); ui.Text(c, "Searching symbols…").TextColor(colors(c).muted) })
				case symbolMode && q.symbolErr != nil:
					ui.Text(c, q.symbolErr.Error()).TextColor(colors(c).err).Padding(8)
				case !commandMode && !symbolMode && q.listing:
					ui.Row(c).Padding(8).Gap(8).Children(func() {
						ui.Spinner(c)
						ui.Text(c, "Listing files…").TextColor(colors(c).muted)
					})
				case !commandMode && !symbolMode && q.err != nil:
					ui.Text(c, q.err.Error()).TextColor(colors(c).err).Padding(8)
				case len(q.results) == 0:
					ui.Text(c, "No Matches").TextColor(colors(c).muted).Padding(8)
				default:
					ui.Box(c).Height(1).FillWidth().Background(colors(c).separator)
					label := "Files"
					if commandMode {
						label = "Commands"
					} else if symbolMode {
						label = "Symbols"
					}
					ui.List(c, &q.list, len(q.results), func(i int) { a.quickRow(c, i) }).Grow(1).FillWidth().MaxHeight(380).Label(label)
				}
			})
		})
	})
}

func (a *app) quickRow(c *ui.Context, i int) {
	q := &a.quick
	r := q.results[i]
	if strings.HasPrefix(q.query, "#") {
		s := q.symbols[i]
		row := ui.Row(c.Key(i)).FillWidth().Padding(8).Gap(8).Radius(8).AlignItems(ui.Center)
		if i == q.sel {
			row.Background(c.Theme().Accent)
		}
		row.OnClick(func() { a.chooseQuick(i) })
		row.Children(func() {
			ui.Text(c, s.name).Grow(1).SingleLine()
			ui.Text(c, fmt.Sprintf("%s:%d", workspace.RelPath(a.root, s.target.path), s.target.pos.Line+1)).TextColor(colors(c).muted).SingleLine()
		})
		return
	}
	if strings.HasPrefix(q.query, ">") {
		row := ui.Row(c.Key(r.Path)).FillWidth().Padding(8).Gap(8).Radius(8).AlignItems(ui.Center)
		if i == q.sel {
			row.Background(c.Theme().Accent)
		}
		row.OnClick(func() { a.chooseQuick(i) })
		row.Children(func() {
			ui.Text(c, r.Path).Grow(1).SingleLine()
			for _, cmd := range commands() {
				if cmd.name == r.Path && cmd.key != "" {
					ui.Text(c, keyLabel(cmd.mods, cmd.key)).TextColor(colors(c).muted)
					break
				}
			}
		})
		return
	}
	dir, name := filepath.Split(filepath.FromSlash(r.Path))
	row := ui.Row(c).FillWidth().Padding(5, 8).Gap(10).AlignItems(ui.Center).Radius(8)
	if i == q.sel {
		row.Background(c.Theme().Accent)
	}
	row.OnClick(func() { a.chooseQuick(i) })
	row.Children(func() {
		fileIcon(c, name, 22)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			nameStart := len(r.Path) - len(name)
			highlightText(c, name, r.Hits, nameStart, i == q.sel)
			sub := ui.Text(c, strings.TrimSuffix(filepath.ToSlash(dir), "/")).FontSize(11).SingleLine()
			if i == q.sel {
				sub.TextColor(ui.RGBA(0xff, 0xff, 0xff, 0.75))
			} else {
				sub.TextColor(colors(c).muted)
			}
		})
	})
}

// highlightText shows name with the characters the query matched bold,
// hits being byte offsets of the whole path and name starting at off.
func highlightText(c *ui.Context, name string, hits []int, off int, selected bool) {
	color := colors(c).text
	if selected {
		color = ui.RGB(0xff, 0xff, 0xff)
	}
	ui.Row(c).Children(func() {
		run, bold := "", false
		flush := func() {
			if run == "" {
				return
			}
			t := ui.Text(c, run).FontSize(13).TextColor(color).SingleLine()
			if bold {
				t.Bold().Underline()
			}
			run = ""
		}
		for i, r := range name {
			hit := slices.Contains(hits, off+i)
			if hit != bold {
				flush()
				bold = hit
			}
			run += string(r)
		}
		flush()
	})
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}

func size(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
