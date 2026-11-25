package app

import (
	"fmt"
	"path/filepath"
	"see/locales"
	"slices"
	"strings"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/workspace"
)

// quickOpenPanel searches files, or commands when the query starts with >.
func (a *App) quickOpenPanel(c *ui.Context) {
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
				Material(popoverGlass()).Border(1, colors(c).PopoverEdge).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.5))
			panel.OnClick(func() {}) // clicks on the panel stay on it
			panel.Children(func() {
				ui.Row(c).Padding(4, 8).Gap(8).AlignItems(ui.Center).Children(func() {
					symbolIcon(c, icSearch, 20, colors(c).Muted)
					in := ui.TextInput(c, &q.query).Label(locales.MsgOpenQuickly()).Placeholder(locales.MsgOpenQuickly()).FontSize(20).Grow(1).AutoFocus().Background(ui.Transparent).BorderWidth(0)
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
				a.rankQuick()
				commandMode := strings.HasPrefix(q.query, ">")
				_, _, targeted := splitQuickFileSymbol(q.query)
				fileSymbolMode := strings.HasPrefix(q.query, "@") || targeted
				symbolMode := strings.HasPrefix(q.query, "#") || fileSymbolMode
				lineMode := strings.HasPrefix(q.query, ":")
				if targeted && q.filePath != "" {
					ui.Row(c).Padding(5, 8).Gap(8).AlignItems(ui.Center).Children(func() {
						fileIcon(c, filepath.Base(q.filePath), 16)
						ui.Text(c, workspace.RelPath(a.root, q.filePath)).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MinWidth(0)
					})
				}
				switch {
				case fileSymbolMode && !targeted && a.activeEditor() == nil:
					ui.Text(c, locales.Catalog.Format(i18n.Locale(), "fileSymbolsOpenText", nil)).TextColor(colors(c).Muted).Padding(8)
				case lineMode && len(q.results) == 0:
					ui.Text(c, locales.Catalog.Format(i18n.Locale(), "goToLineHint", nil)).TextColor(colors(c).Muted).Padding(8)
				case !commandMode && !lineMode && a.root == "":
					ui.Text(c, locales.MsgOpenAFolderToSearchFilesOrType()).TextColor(colors(c).Muted).Padding(8)
				case symbolMode && !a.cfg.IsTrusted(a.root):
					if ui.Button(c, locales.MsgTrustFolderToSearchSymbols()).Clicked() {
						query := q.query
						q.open = false
						a.trustAsk = true
						a.afterTrust = func() { a.openQuickPanel(query) }
					}
				case symbolMode && !fileSymbolMode && strings.TrimSpace(q.query[1:]) == "":
					ui.Text(c, locales.MsgTypeASymbolNameToSearchTheWorkspace()).TextColor(colors(c).Muted).Padding(8)
				case symbolMode && q.symbolLoading && len(q.results) == 0:
					ui.Row(c).Padding(8).Gap(8).Children(func() { ui.Spinner(c); ui.Text(c, locales.MsgSearchingSymbols()).TextColor(colors(c).Muted) })
				case symbolMode && q.symbolErr != nil:
					ui.Text(c, q.symbolErr.Error()).TextColor(colors(c).Err).Padding(8)
				case !commandMode && !symbolMode && !lineMode && q.listing:
					ui.Row(c).Padding(8).Gap(8).Children(func() {
						ui.Spinner(c)
						ui.Text(c, locales.MsgListingFiles()).TextColor(colors(c).Muted)
					})
				case !commandMode && !symbolMode && !lineMode && q.err != nil:
					ui.Text(c, q.err.Error()).TextColor(colors(c).Err).Padding(8)
				case len(q.results) == 0:
					ui.Text(c, locales.MsgNoMatches()).TextColor(colors(c).Muted).Padding(8)
				default:
					ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator)
					label := locales.Files()
					if commandMode {
						label = "Commands"
					} else if symbolMode {
						label = "Symbols"
					}
					list := ui.List(c, &q.list, len(q.results), func(i int) { a.quickRow(c, i) }).Grow(1).FillWidth().MaxHeight(380).Label(label)
					if symbolMode {
						list.Height(float32(min(len(q.results), 10)) * 36)
					}
				}
			})
		})
	})
}

func (a *App) quickRow(c *ui.Context, i int) {
	q := &a.quick
	r := q.results[i]
	_, _, targeted := splitQuickFileSymbol(q.query)
	if strings.HasPrefix(q.query, "#") || strings.HasPrefix(q.query, "@") || targeted {
		s := q.symbols[i]
		row := ui.Row(c.Key(i)).FillWidth().Padding(8).Gap(8).Radius(8).AlignItems(ui.Center)
		if i == q.sel {
			row.Background(c.Theme().Accent)
		}
		row.OnClick(func() { a.chooseQuick(i) })
		row.Children(func() {
			ui.Text(c, s.name).Grow(1).SingleLine().Ellipsis("…").MinWidth(0)
			ui.Text(c, fmt.Sprintf("%s:%d", workspace.RelPath(a.root, s.target.path), s.target.pos.Line+1)).TextColor(colors(c).Muted).SingleLine().Ellipsis("…")
		})
		return
	}
	if strings.HasPrefix(q.query, ">") || strings.HasPrefix(q.query, ":") {
		enabled := true
		shortcut, id := "", ""
		if i < len(q.commands) {
			cmd := q.commands[i]
			enabled = a.commandEnabled(cmd.id)
			id = cmd.id
			if cmd.key != "" {
				shortcut = keyLabel(cmd.mods, cmd.key)
			}
		}
		row := ui.Row(c.Key(r.Path)).FillWidth().Padding(8).Gap(8).Radius(8).AlignItems(ui.Center).Disabled(!enabled)
		if i == q.sel {
			row.Background(c.Theme().Accent)
		}
		row.OnClick(func() { a.chooseQuick(i) })
		row.Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				ui.Text(c, r.Path).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0)
				if id != "" {
					color := colors(c).Muted
					if i == q.sel {
						color = ui.RGBA(0xff, 0xff, 0xff, 0.75)
					}
					ui.Text(c, id).FontSize(11).TextColor(color).SingleLine().Ellipsis("…").MinWidth(0)
				}
			})
			if shortcut != "" {
				ui.Text(c, shortcut).TextColor(colors(c).Muted)
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
			color := colors(c).Muted
			if i == q.sel {
				color = ui.RGBA(0xff, 0xff, 0xff, 0.75)
			}
			highlightTextStyle(c, strings.TrimSuffix(filepath.ToSlash(dir), "/"), r.Hits, 0, 11, color)
		})
	})
}

// highlightText shows name with the characters the query matched bold,
// hits being byte offsets of the whole path and name starting at off.
func highlightText(c *ui.Context, name string, hits []int, off int, selected bool) {
	color := colors(c).Text
	if selected {
		color = ui.RGB(0xff, 0xff, 0xff)
	}
	highlightTextStyle(c, name, hits, off, 13, color)
}

func highlightTextStyle(c *ui.Context, name string, hits []int, off int, fontSize float32, color ui.Color) {
	ui.Row(c).Children(func() {
		run, bold := "", false
		flush := func() {
			if run == "" {
				return
			}
			t := ui.Text(c, run).FontSize(fontSize).TextColor(color).SingleLine().Ellipsis("…")
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
