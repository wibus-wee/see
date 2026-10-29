package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
	"golang.org/x/net/html"
)

const maxMarkdownImageBytes = 20 << 20

var markdownHTTPClient = &http.Client{Timeout: 15 * time.Second}

func markdownImages(dir string) func(string) (ui.ImageSource, error) {
	local := markdown.FileImages(dir)
	return func(src string) (ui.ImageSource, error) {
		u, err := url.Parse(src)
		if err != nil {
			return nil, err
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			decoded, err := url.PathUnescape(src)
			if err != nil {
				return nil, err
			}
			return local(decoded)
		}
		resp, err := markdownHTTPClient.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("image request: %s", resp.Status)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxMarkdownImageBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxMarkdownImageBytes {
			return nil, fmt.Errorf("image exceeds %d bytes", maxMarkdownImageBytes)
		}
		if strings.EqualFold(filepath.Ext(u.Path), ".svg") || strings.HasPrefix(strings.TrimSpace(string(data)), "<svg") || strings.Contains(resp.Header.Get("Content-Type"), "image/svg+xml") {
			return ui.ParseSVG(data)
		}
		return ui.DecodeBitmap(data)
	}
}

// standaloneHTMLImage accepts a single img tag without treating arbitrary HTML as UI.
func standaloneHTMLImage(source string) (src, alt string, ok bool) {
	z := html.NewTokenizer(strings.NewReader(source))
	found := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return src, alt, found && z.Err() == io.EOF && src != ""
		case html.TextToken:
			if strings.TrimSpace(string(z.Text())) != "" {
				return "", "", false
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tag := z.Token()
			if found || tag.Data != "img" {
				return "", "", false
			}
			found = true
			for _, attr := range tag.Attr {
				switch attr.Key {
				case "src":
					src = attr.Val
				case "alt":
					alt = attr.Val
				}
			}
		default:
			return "", "", false
		}
	}
}

func markdownOptions(dir string) *markdown.Options {
	o := &markdown.Options{LoadImage: markdownImages(dir)}
	o.Block = func(c *ui.Context, d *parser.Document, i int) bool {
		kind := d.Block(i).Kind
		if kind != parser.HTMLBlock && kind != parser.Paragraph {
			return false
		}
		src, alt, ok := standaloneHTMLImage(d.Lines(i))
		if !ok {
			return false
		}
		// Angle destinations keep parentheses and spaces from changing the syntax.
		src = strings.NewReplacer("\\", "%5C", "<", "%3C", ">", "%3E", " ", "%20", "\n", "%0A").Replace(src)
		alt = strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]").Replace(alt)
		markdown.View(c, "!["+alt+"](<"+src+">)", o).FillWidth()
		return true
	}
	return o
}

func isMarkdown(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".md" || ext == ".markdown" || ext == ".mdown" || ext == ".mkd"
}

func (a *app) openMarkdownTab(g *group) {
	if g == nil {
		return
	}
	path := tabPath(g.active)
	if !isMarkdown(path) || a.docs[path] == nil {
		return
	}
	key := markdownTab(path)
	if !g.has(key) {
		at := len(g.order)
		for i, tab := range g.order {
			if tab == g.active {
				at = i + 1
				break
			}
		}
		g.addTab(key, at)
	}
	a.activateIn(g, key)
	a.showLoaded(g, a.docs[path])
}

func (a *app) markdownView(c *ui.Context, g *group, d *doc) {
	if d.markdownOptions == nil {
		d.markdownOptions = markdownOptions(filepath.Dir(d.path))
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
