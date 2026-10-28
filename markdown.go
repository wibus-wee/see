package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown" || ext == ".mdown" || ext == ".mkd"
}

func (a *app) toggleMarkdownPreview(g *group) {
	if g == nil || !isMarkdown(g.active) || a.docs[g.active] == nil {
		return
	}
	if g.markdownPreview == nil {
		g.markdownPreview = make(map[string]bool)
	}
	g.markdownPreview[g.active] = !g.markdownPreview[g.active]
	if !g.markdownPreview[g.active] {
		a.focusEditor = g.eds[g.active]
	}
}

func (a *app) markdownView(c *ui.Context, g *group, d *doc) {
	if d.markdownOptions == nil {
		d.markdownOptions = &markdown.Options{LoadImage: markdown.FileImages(filepath.Dir(d.path))}
	}
	if v := d.buf.Version(); !d.markdownCached || v != d.markdownVersion {
		d.markdownText, d.markdownVersion, d.markdownCached = d.buf.String(), v, true
	}
	scroll := ui.Scroll(c.Key(d.path)).Grow(1).FillWidth()
	if scroll.FocusWithin() && a.focus != g {
		a.focus = g
	}
	scroll.Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(32, 40, 64, 40).Children(func() {
			markdown.View(c, d.markdownText, d.markdownOptions).FillWidth().MaxWidth(850)
		})
	})
}
