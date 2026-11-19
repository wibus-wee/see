package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/locales"
)

// counterpartNames puts each family in a fixed order so cycling from
// any member finds the same next and previous files.
func counterpartNames(name string) []string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	var suffixes []string
	switch ext {
	case ".go":
		stem = strings.TrimSuffix(stem, "_test")
		if strings.HasSuffix(stem, "_test") {
			return nil
		}
		suffixes = []string{".go", "_test.go"}
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		if base, ok := strings.CutSuffix(stem, ".test"); ok {
			stem = base
		} else {
			stem = strings.TrimSuffix(stem, ".spec")
		}
		if strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") {
			return nil
		}
		suffixes = []string{ext, ".test" + ext, ".spec" + ext}
	case ".swift":
		stem = strings.TrimSuffix(stem, "Tests")
		if strings.HasSuffix(stem, "Tests") {
			return nil
		}
		suffixes = []string{".swift", "Tests.swift"}
	case ".h", ".hh", ".hpp", ".hxx", ".c", ".cc", ".cpp", ".cxx", ".m", ".mm":
		suffixes = []string{".h", ".hh", ".hpp", ".hxx", ".c", ".cc", ".cpp", ".cxx", ".m", ".mm"}
	}
	if stem == "" {
		return nil
	}
	for i, suffix := range suffixes {
		suffixes[i] = stem + suffix
	}
	return suffixes
}

func counterpartPaths(path string, names []string) []string {
	dir := filepath.Dir(path)
	var paths []string
	for _, name := range names {
		p := filepath.Join(dir, name)
		if p == path {
			paths = append(paths, p)
		} else if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			paths = append(paths, p)
		}
	}
	return paths
}

func (a *App) counterpartNames(g *group) []string {
	if d := a.docs[g.active]; d != nil && !d.untitled {
		return counterpartNames(filepath.Base(d.path))
	}
	return nil
}

func (a *App) switchCounterpart(step int) {
	g := a.focus
	names := a.counterpartNames(g)
	if len(names) == 0 {
		a.counterpartMiss = a.activePath()
		return
	}
	path := g.active
	a.cancelNav()
	seq := a.navSeq
	go func() {
		paths := counterpartPaths(path, names)
		a.post.Post(func() {
			if a.navSeq != seq || a.focus != g || g.active != path {
				return
			}
			if i := slices.Index(paths, path); len(paths) > 1 && i >= 0 {
				a.jumpToCounterpart(g, paths[(i+step+len(paths))%len(paths)])
			} else {
				a.counterpartMiss = path
			}
		})
	}()
}

func (a *App) jumpToCounterpart(g *group, path string) {
	a.counterpartMiss = ""
	a.jumpToFile(g, path)
	a.requestGroupFocus(g)
}

type counterpartMenu struct {
	source string
	paths  []string
}

func (a *App) addCounterpartsMenu(m *ui.Menu, g *group) {
	label := locales.MsgCounterparts()
	path := g.active
	if g.counterpartMenu == nil || g.counterpartMenu.source != path {
		result := &counterpartMenu{source: path}
		g.counterpartMenu = result
		if names := a.counterpartNames(g); len(names) != 0 {
			go func() {
				paths := counterpartPaths(path, names)
				a.post.Post(func() {
					if g.counterpartMenu == result {
						result.paths = slices.DeleteFunc(paths, func(p string) bool { return p == path })
					}
				})
			}()
		}
	}
	paths := g.counterpartMenu.paths
	if len(paths) == 0 {
		m.Item(label).Disabled(true)
		return
	}
	m.Submenu(label, func(m *ui.Menu) {
		m.MaxWidth(320)
		for _, p := range paths {
			name := filepath.Base(p)
			if m.Item(name).ID(p).Content(func(x ui.MenuItemContent) {
				ui.Row(x.Context).Gap(6).AlignItems(ui.Center).Children(func() {
					fileIcon(x.Context, name, 13)
					ui.Text(x.Context, name).FontSize(13).TextColor(x.Text).SingleLine().Shrink(1)
				})
			}).Chosen() {
				a.jumpToCounterpart(g, p)
			}
		}
	})
}
