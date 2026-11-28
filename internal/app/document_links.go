package app

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/workspace"
	"see/internal/uri"
)

// documentLinkClient is a language client that answers document link
// requests, as gopls does for import paths and //go:embed files.
type documentLinkClient interface {
	HasDocumentLinks() bool
	DocumentLinkResolveSupport() bool
	DocumentLinks(ctx context.Context, uri string) ([]lsp.DocumentLink, error)
	DocumentLinkResolve(ctx context.Context, link *lsp.DocumentLink) (*lsp.DocumentLink, error)
}

// docLink is a link of a document in buffer offsets of the version it
// was fetched of; linkBufVer holds it, and an edit since takes the list
// off until the next answer lands.
type docLink struct {
	start, end int // byte offsets, as editor.Diagnostic's
	target     string
	data       lsp.DocumentLink // for documentLink/resolve when target is empty
}

// docLinkSettle is how long the buffer must rest before links refresh;
// shorter than inlaySettle's, as links refresh only on edits, not scrolls.
const docLinkSettle = 150 * time.Millisecond

// syncDocumentLinks asks the document's server for its links when the
// buffer's version has moved on from the one links are of. It runs every
// frame the document builds, as syncInlayHints does; a reply that
// arrives after the buffer moved on is dropped. A Markdown file no
// server claims gets its links from the parser, which sees the buffer as
// it is, so no waiting applies.
func (a *App) syncDocumentLinks(d *doc, st *editor.State) {
	m := a.serverFor(d.path)
	served := m != nil && a.workspaceTrusted()
	if !served && isMarkdown(d.path) && d.buf != nil && !d.lossy && !d.plain && st != nil {
		// linkVersion cannot mark a parsed version: a fresh buffer's is 0,
		// as linkVersion's. linkLocal says the parse ran for linkBufVer.
		if v := d.buf.Version(); !d.linkLocal || d.linkBufVer != v {
			d.docLinks = markdownDocLinks(d.buf.String())
			d.linkBufVer, d.linkVersion, d.linkLocal = v, v, true
			d.linkSeq = 0 // no request is out to drop or to guard
		}
		return
	}
	if !served || d.buf == nil || d.lossy || d.plain || st == nil || a.wsCtx == nil {
		// Clear only the lane the requests filled: linkSeq > 0 marks it,
		// and nothing unserved reaches here having asked for links.
		if d.linkSeq > 0 {
			a.clearDocumentLinks(d)
		}
		return
	}
	version := d.buf.Version()
	if version == d.linkVersion && d.linkSeq > 0 {
		return // fetched, fetching, or already tried for this text
	}
	d.linkSeq++
	if d.linkTimer != nil {
		d.linkTimer.Stop()
	}
	if d.linkCancel != nil {
		d.linkCancel()
	}
	d.linkVersion = version
	seq, gen := d.linkSeq, a.gen
	ctx, cancel := context.WithCancel(a.wsCtx)
	d.linkCancel = cancel
	snap, docURI := d.buf.Snapshot(), d.uri
	d.linkTimer = time.AfterFunc(docLinkSettle, func() {
		a.post.Post(func() {
			if ctx.Err() != nil || a.gen != gen || a.docs[d.path] != d || d.linkSeq != seq || d.buf.Version() != version {
				return
			}
			go a.fetchDocumentLinks(ctx, d, m, snap, docURI, seq, gen)
		})
	})
}

// fetchDocumentLinks requests the links and posts them, all guards
// holding: same workspace generation, document, request and text.
func (a *App) fetchDocumentLinks(ctx context.Context, d *doc, m *langserver.Server, snap editor.Snapshot, docURI string, seq, gen int) {
	defer func() {
		a.post.Post(func() {
			if a.docs[d.path] == d && d.linkSeq == seq {
				d.linkCancel = nil
			}
		})
	}()
	c, err := m.Get(ctx)
	if err != nil {
		return
	}
	linksClient, ok := c.(documentLinkClient)
	if !ok || !linksClient.HasDocumentLinks() {
		return
	}
	if err := m.Sync(ctx, c, docURI, snap); err != nil {
		return
	}
	enc := c.Encoding()
	raw, err := linksClient.DocumentLinks(ctx, docURI)
	if err != nil {
		return // a failed link is invisible, not an error the reader asked for
	}
	resolve := linksClient.DocumentLinkResolveSupport()
	// A server that resolves lazily gets a bounded eager pass: under the
	// limit every target arrives before a click could ask for it.
	if resolve && len(raw) > 0 && len(raw) <= 64 {
		for i := range raw {
			if raw[i].Target != "" {
				continue
			}
			if r, err := linksClient.DocumentLinkResolve(ctx, &raw[i]); err == nil && r != nil {
				raw[i] = *r
			}
			if ctx.Err() != nil {
				return
			}
		}
	}
	links := toDocLinks(raw, snap, enc)
	a.post.Post(func() {
		if a.gen != gen || a.docs[d.path] != d || d.linkSeq != seq || d.buf == nil {
			return
		}
		// The buffer may have moved since snap; the links stay of it,
		// linkBufVer keeps them off the new text until the next answer.
		d.docLinks = links
		d.linkBufVer = snap.Version()
		d.linkVersion = d.buf.Version() // refreshed; hold for this version
		if d.linkVersion != snap.Version() {
			d.linkVersion = 0 // stale for the new text: ask again next frame
		}
	})
}

// toDocLinks turns the server's links into doc links in buffer offsets.
// A range off the text, as of a version the server had that is not the
// snapshot's, is dropped rather than misdrawn.
func toDocLinks(raw []lsp.DocumentLink, snap editor.Snapshot, enc string) []docLink {
	out := make([]docLink, 0, len(raw))
	for _, l := range raw {
		r := l.Range
		if r.Start.Line < 0 || r.Start.Line >= snap.LineCount() || r.End.Line < 0 || r.End.Line >= snap.LineCount() || r.Start.Line > r.End.Line {
			continue
		}
		start := snap.LineStart(r.Start.Line) + lsp.ColumnFromLSP(snap.Line(r.Start.Line), r.Start.Character, enc)
		end := snap.LineStart(r.End.Line) + lsp.ColumnFromLSP(snap.Line(r.End.Line), r.End.Character, enc)
		end = min(end, snap.LineEnd(r.End.Line))
		if end <= start {
			continue
		}
		out = append(out, docLink{start: start, end: end, target: l.Target, data: l})
	}
	return out
}

// linkBackgrounds underlays each link's range in the link color: it is
// translucent enough to read as an underline over the text.
func (a *App) linkBackgrounds(st *editor.State, d *doc) {
	if len(d.docLinks) == 0 || d.buf == nil || d.buf.Version() != d.linkBufVer {
		return // links of an older text, as a fetch not yet back, stay off
	}
	link := a.colors().Link
	bgs := make([]editor.TextBackground, 0, len(d.docLinks)+len(st.TextBackgrounds))
	for _, l := range d.docLinks {
		bgs = append(bgs, editor.TextBackground{Start: l.start, End: l.end, Color: link})
	}
	bgs = append(bgs, st.TextBackgrounds...)
	st.TextBackgrounds = bgs
}

// linkAt returns the link covering byte offset off, as the editor
// computes it for OnDefinition; a click on a link's middle or its last
// character names it.
func linkAt(d *doc, off int) *docLink {
	if d.buf != nil && d.buf.Version() != d.linkBufVer {
		return nil // stale for the current text, as an edit since the fetch
	}
	for i := range d.docLinks {
		l := &d.docLinks[i]
		if off >= l.start && off <= l.end {
			return l
		}
	}
	return nil
}

// openDocLinkTarget opens a link's target: a file of the project in a
// tab, a URL in the browser. A link the server left unresolved is asked
// for its target first.
func (a *App) openDocLinkTarget(d *doc, l *docLink) {
	if l.target == "" {
		a.resolveAndOpenDocLink(d, l)
		return
	}
	a.followDocLink(d, l.target)
}

// resolveAndOpenDocLink asks the server for a link's target before
// opening it, for a server that resolves lazily and the eager pass did
// not reach.
func (a *App) resolveAndOpenDocLink(d *doc, l *docLink) {
	m := a.serverFor(d.path)
	if m == nil || !a.workspaceTrusted() {
		return
	}
	gen, seq := a.gen, d.linkSeq
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	go func() {
		defer cancel()
		c, err := m.Get(ctx)
		if err != nil {
			return
		}
		lc, ok := c.(documentLinkClient)
		if !ok || !lc.DocumentLinkResolveSupport() {
			return
		}
		if err := m.Sync(ctx, c, d.uri, d.buf.Snapshot()); err != nil {
			return
		}
		resolved, err := lc.DocumentLinkResolve(ctx, &l.data)
		if err != nil || resolved == nil || resolved.Target == "" {
			return
		}
		target := resolved.Target
		a.post.Post(func() {
			if a.gen != gen || a.docs[d.path] != d || d.linkSeq != seq {
				return
			}
			l.target = target
			a.followDocLink(d, target)
		})
	}()
}

// markdownDocLinks returns the links of a Markdown source as doc links:
// each link's and image's whole span, as the parser reports no range of
// the destination alone, so Cmd+click anywhere on [text](dest) follows
// the link as the preview's click does. The Default extensions are the
// view's, so what underlines is what rendered as a link; definitions
// have no target, so a reference link stays underlined at its use.
func markdownDocLinks(src string) []docLink {
	d := parser.Parse(src)
	var links []docLink
	for i := 1; i < d.Len(); i++ {
		for _, in := range d.Inlines(i) {
			if (in.Kind == parser.Link || in.Kind == parser.Image) && !in.Pending && in.Dest != "" && in.End > in.Start {
				links = append(links, docLink{start: in.Start, end: in.End, target: in.Dest})
			}
		}
	}
	return links
}

// markdownAnchorLine returns the 0-based line of the heading whose slug,
// as GitHub makes it, is name, or -1: where a README.md#install-style
// fragment lands when the target opens as text.
func markdownAnchorLine(src, name string) int {
	d := parser.Parse(src)
	for i := 1; i < d.Len(); i++ {
		b := d.Block(i)
		if b.Kind != parser.Heading || markdown.Slug(parser.PlainText(d.Inlines(i))) != strings.ToLower(name) {
			continue
		}
		return strings.Count(src[:b.Start], "\n")
	}
	return -1
}

// followDocLink opens a resolved target by its scheme: file opens in a
// tab at the link's position where it has one, http and https in the
// browser, and a bare path relative to the document's directory.
func (a *App) followDocLink(d *doc, target string) {
	// No destination a text wrote is trusted: a control byte or a
	// backslash makes it open nothing, as ui.linkAllowed reads it.
	if strings.ContainsAny(target, "\x00\\") {
		return
	}
	u, err := url.Parse(target)
	if err != nil {
		return
	}
	// A Windows path parses as a one-letter scheme: c:/x.md as scheme c
	// and path /x.md, c:x.md as scheme c and opaque x.md. It is a local
	// path, not a URI of another scheme.
	if len(u.Scheme) == 1 {
		u.Path, u.Scheme = u.Scheme+":"+u.Opaque+u.Path, ""
		u.Opaque = ""
	}
	switch u.Scheme {
	case "http", "https":
		a.services.OpenURL(target)
	case "file", "":
		var path string
		if u.Scheme == "file" {
			p, err := uri.ToPath(target)
			if err != nil {
				a.services.OpenURL(target) // a host of its own, as a remote file
				return
			}
			path = p
		} else {
			// A bare or relative path, as //go:embed's or a server's
			// relative one, is relative to the document's directory; a
			// fragment alone names a heading or line of the document
			// itself.
			if u.Path == "" {
				path = d.path
			} else {
				p, err := url.PathUnescape(u.Path)
				if err != nil {
					return
				}
				// A POSIX root in a file URI is a Windows drive, as
				// file:///c:/x.md's /c:/x.md; other POSIX roots are
				// absolute paths already.
				if len(p) >= 3 && p[0] == '/' && p[2] == ':' && filepath.Separator == '\\' {
					p = p[1:]
				}
				path = p
				if !filepath.IsAbs(path) {
					path = filepath.Join(filepath.Dir(d.path), path)
				}
				path = filepath.Clean(path)
			}
		}
		// A link can carry a position, as file:///x.go#L12 or #L12:8, or
		// a heading's slug, as README.md#install.
		line, col := linkAnchor(u)
		if line == 0 && u.Fragment != "" && isMarkdown(path) {
			line = a.markdownHeadingLine(path, u.Fragment) + 1
		}
		a.cancelNav()
		var pos *editor.Pos
		if line > 0 {
			pos = &editor.Pos{Line: line - 1, Col: col}
		}
		a.openFile(path, pos)
	default:
		// mailto goes to the system; anything else, as javascript:, does
		// not, the schemes SafeToOpen allows.
		if ui.SafeToOpen(target) {
			a.services.OpenURL(target)
		}
	}
}

// markdownHeadingLine reads the file of path and finds the line its
// heading named name is on, for a link's #a-heading fragment. The buffer
// answers when the file is open, as its edits are the text the writer
// saw; a read miss or no such heading lands the link at the top.
func (a *App) markdownHeadingLine(path, name string) int {
	if t := a.docs[path]; t != nil && t.buf != nil {
		return markdownAnchorLine(t.buf.String(), name)
	}
	// The size is checked before reading, not after: a file over the
	// cap must not be held in memory only to be dropped.
	if fi, err := os.Stat(path); err != nil || fi.Size() > workspace.MaxSize {
		return -1
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > workspace.MaxSize {
		return -1
	}
	return markdownAnchorLine(string(data), name)
}

// linkAnchor reads the line and column of a link's fragment, as gopls
// and clangd write them: #L12 or #L12:8, 1-based.
func linkAnchor(u *url.URL) (line, col int) {
	f := strings.TrimPrefix(u.Fragment, "L")
	if f == u.Fragment {
		return 0, 0
	}
	ls, cs, _ := strings.Cut(f, ":")
	if n, err := strconv.Atoi(ls); err == nil && n > 0 {
		line = n
	}
	if n, err := strconv.Atoi(cs); err == nil && n > 0 {
		col = n - 1
	}
	return line, col
}

// clearDocumentLinks drops a document's links and whatever was fetching
// more, as clearInlayHints does its lane.
func (a *App) clearDocumentLinks(d *doc) {
	// linkSeq of 0 both drops a reply still on its way, whose captured
	// seq is higher, and lets syncDocumentLinks ask again.
	d.linkSeq = 0
	if d.linkTimer != nil {
		d.linkTimer.Stop()
		d.linkTimer = nil
	}
	if d.linkCancel != nil {
		d.linkCancel()
		d.linkCancel = nil
	}
	d.docLinks, d.linkVersion, d.linkBufVer, d.linkLocal = nil, 0, 0, false
}
