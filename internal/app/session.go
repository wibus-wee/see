package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
	"see/internal/git"
	"see/internal/nav"
)

func (a *App) resizedWorkspace(width, height int) {
	if a.root == "" || width < 480 || height < 320 || width > 10000 || height > 10000 {
		return
	}
	if a.windowWidth != width || a.windowHeight != height {
		a.windowWidth, a.windowHeight = width, height
		a.saveConfigSoon()
	}
}

func (a *App) captureSession() {
	var snapshot func(*layout) *config.Layout
	snapshot = func(n *layout) *config.Layout {
		if n == nil {
			return nil
		}
		if n.g == nil {
			return &config.Layout{Vertical: n.vertical, Percent: n.pct, First: snapshot(n.a), Second: snapshot(n.b)}
		}
		g := n.g
		s := &config.Layout{Active: tabPath(g.active), ActivePreview: isMarkdownTab(g.active), Preview: g.preview, Focused: g == a.focus}
		if isDiffTab(g.active) {
			if v := g.diffs[g.active]; v != nil && (v.localHistory != nil || v.revision != nil) {
				s.Active = ""
			} else {
				s.Active = g.active
			}
		}
		for _, key := range g.order {
			path := tabPath(key)
			if _, discarded := a.discardedSessionDocs[path]; discarded {
				continue
			}
			if a.root == "" {
				d := a.docs[path]
				_, recovering := a.standaloneSessionDocs[path]
				// A standalone session is crash recovery, not a recent-files
				// list: keep untitled drafts and saved files with unsaved edits.
				// Keep a restored entry while its recovery record is still loading.
				if d == nil || (!d.untitled && (d.buf == nil || !d.buf.Modified()) && !(recovering && d.loading)) {
					continue
				}
			}
			if isDiffTab(key) {
				if v := g.diffs[key]; v != nil && (v.localHistory != nil || v.revision != nil) {
					continue
				}
				if v := g.diffs[key]; v != nil && v.root != "" && v.root != a.git.root {
					continue
				}
				if spec := diffTabSpec(g.diffs[key]); spec != nil {
					s.Tabs = append(s.Tabs, config.Tab{Diff: spec})
				}
				continue
			}
			tab := config.Tab{Path: path, MarkdownPreview: !isMarkdownTab(key) && g.markdownPreview[path], PreviewTab: isMarkdownTab(key)}
			if d := a.docs[path]; d != nil && d.untitled {
				tab.Untitled, tab.Name = true, d.name
			}
			if ed := g.eds[path]; ed != nil {
				tab.Folds = append([]int{}, ed.Folded()...)
				p := ed.CursorPos()
				tab.Line, tab.Column = p.Line, p.Col
			} else if p := g.pending[path].pos; p != nil {
				tab.Line, tab.Column = p.Line, p.Col
				tab.Folds = g.pending[path].folds
			}
			s.Tabs = append(s.Tabs, tab)
		}
		return s
	}
	if a.cfg.Workspaces == nil {
		a.cfg.Workspaces = make(map[string]config.Workspace)
	}
	workspace := config.Workspace{Layout: snapshot(a.layout), SidebarHidden: a.sidebarOff, Navigator: a.navigator, ChangesClosed: a.git.changesClosed, StashesClosed: a.git.stashesClosed, HistoryClosed: a.git.historyClosed, GitSplit: a.git.split, GitLogSplit: a.git.logSplit, WindowWidth: a.windowWidth, WindowHeight: a.windowHeight, Bookmarks: a.sessionBookmarks()}
	if a.regexToolWindow.initialized {
		workspace.RegexToolX, workspace.RegexToolY = a.regexToolWindow.rect.X, a.regexToolWindow.rect.Y
		workspace.RegexToolWidth, workspace.RegexToolHeight = a.regexToolWindow.rect.W, a.regexToolWindow.rect.H
	}
	workspace.Terminals, workspace.ActiveTerminal, workspace.PanelOpen = a.termSession()
	if a.root == "" {
		a.mergeStandaloneSession(workspace)
		return
	}
	a.cfg.Workspaces[a.root] = workspace
}

// mergeStandaloneSession merges this window's recovery tabs into the one
// schema slot available for folderless windows. Paths previously contributed
// by this window are replaced; paths belonging to other windows are retained.
// The merged session is deliberately one group so the primary can recover all
// documents after a restart.
func (a *App) mergeStandaloneSession(current config.Workspace) {
	previous := a.cfg.Workspaces[""]
	var merged []config.Tab
	seen := make(map[string]bool)
	appendTab := func(tab config.Tab) {
		key := tab.Path
		if tab.PreviewTab {
			key += "\x00preview"
		}
		if tab.Diff != nil || tab.Path == "" || seen[key] {
			return
		}
		seen[key] = true
		merged = append(merged, tab)
	}
	walkSessionTabs(previous.Layout, func(tab config.Tab) {
		_, owned := a.standaloneSessionDocs[tab.Path]
		_, discarded := a.discardedSessionDocs[tab.Path]
		if !owned && !discarded {
			appendTab(tab)
		}
	})
	var active string
	var activePreview bool
	walkSessionTabs(current.Layout, func(tab config.Tab) {
		a.standaloneSessionDocs[tab.Path] = struct{}{}
		appendTab(tab)
	})
	if current.Layout != nil {
		active, activePreview = standaloneActive(current.Layout)
	}

	if len(merged) == 0 {
		previous.Layout = nil
	} else {
		if active == "" {
			active, activePreview = standaloneActive(previous.Layout)
		}
		previous.Layout = &config.Layout{Tabs: merged, Active: active, ActivePreview: activePreview, Focused: true}
	}
	if current.Layout != nil {
		previous.SidebarHidden, previous.Navigator = current.SidebarHidden, current.Navigator
		previous.WindowWidth, previous.WindowHeight = current.WindowWidth, current.WindowHeight
	}
	previous.RegexToolX, previous.RegexToolY = current.RegexToolX, current.RegexToolY
	previous.RegexToolWidth, previous.RegexToolHeight = current.RegexToolWidth, current.RegexToolHeight
	a.cfg.Workspaces[""] = previous
}

func walkSessionTabs(layout *config.Layout, fn func(config.Tab)) {
	if layout == nil {
		return
	}
	for _, tab := range layout.Tabs {
		fn(tab)
	}
	walkSessionTabs(layout.First, fn)
	walkSessionTabs(layout.Second, fn)
}

func standaloneActive(layout *config.Layout) (string, bool) {
	if layout == nil {
		return "", false
	}
	if layout.Focused && layout.Active != "" {
		return layout.Active, layout.ActivePreview
	}
	if active, preview := standaloneActive(layout.First); active != "" {
		return active, preview
	}
	if active, preview := standaloneActive(layout.Second); active != "" {
		return active, preview
	}
	return layout.Active, layout.ActivePreview
}

// sessionBookmarks is what of the workspace's waypoints the session
// keeps: those of files in it.
func (a *App) sessionBookmarks() []config.Bookmark {
	var out []config.Bookmark
	for _, bm := range a.bookmarks.List {
		if filepath.IsAbs(bm.Path) {
			out = append(out, config.Bookmark{Path: bm.Path, Line: bm.Line})
		}
	}
	return out
}

func (a *App) restoreSession() {
	if a.root == "" && !a.ownsStandaloneSession {
		return
	}
	s, ok := a.cfg.Workspaces[a.root]
	if !ok {
		return
	}
	a.restoreRegexToolWindow(s)
	if s.Layout == nil {
		return
	}
	a.restoringSession = true
	defer func() { a.restoringSession = false }()
	a.sidebarOff = s.SidebarHidden
	if s.Navigator >= navFiles && s.Navigator <= navBookmarks {
		a.navigator = s.Navigator
	}
	a.git.changesClosed, a.git.stashesClosed, a.git.historyClosed, a.git.split, a.git.logSplit = s.ChangesClosed, s.StashesClosed, s.HistoryClosed, s.GitSplit, s.GitLogSplit
	a.openRestoredTerminals(s)
	for _, bm := range s.Bookmarks {
		if filepath.IsAbs(bm.Path) && bm.Line >= 0 {
			a.bookmarks.Add(nav.Bookmark{Path: bm.Path, Line: bm.Line})
		}
	}
	var focused *group
	var restore func(*config.Layout, *layout, int) *layout
	restore = func(saved *config.Layout, parent *layout, depth int) *layout {
		if saved == nil || depth > 32 {
			return nil
		}
		if saved.First != nil && saved.Second != nil {
			n := &layout{parent: parent, vertical: saved.Vertical, pct: saved.Percent}
			if n.pct < 5 || n.pct > 95 {
				n.pct = 50
			}
			n.a, n.b = restore(saved.First, n, depth+1), restore(saved.Second, n, depth+1)
			if n.a == nil || n.b == nil {
				return nil
			}
			return n
		}
		g := a.newGroup()
		n := &layout{g: g, parent: parent}
		for _, tab := range saved.Tabs {
			if tab.Diff != nil {
				a.restoreDiffTab(g, *tab.Diff)
				continue
			}
			key := tab.Path
			if tab.PreviewTab {
				key = markdownTab(tab.Path)
			}
			if !filepath.IsAbs(tab.Path) || g.has(key) || (tab.PreviewTab && !isMarkdown(tab.Path)) {
				continue
			}
			if a.root == "" {
				a.standaloneSessionDocs[tab.Path] = struct{}{}
			}
			// Saved standalone tabs exist only to reach their recovery record.
			// If a save removed that record before the session update made it
			// to disk, do not turn the stale entry into a recent-files reopen.
			if a.root == "" && !tab.Untitled && pendingStandaloneRecovery(tab.Path) == nil {
				continue
			}
			pos := editor.Pos{Line: max(tab.Line, 0), Col: max(tab.Column, 0)}
			if tab.Untitled && a.docs[tab.Path] == nil {
				if !validDraftPath(tab.Path) {
					continue
				}
				d := &doc{path: tab.Path, uri: uriOf(tab.Path), name: tab.Name, untitled: true, cache: &draftCache{}}
				if d.name == "" {
					d.name = "Untitled"
				}
				a.docs[tab.Path] = d
				a.loadDoc(d)
			}
			if tab.PreviewTab {
				d := a.docs[tab.Path]
				if d == nil {
					d = &doc{path: tab.Path, uri: uriOf(tab.Path), name: filepath.Base(tab.Path)}
					a.docs[tab.Path] = d
					a.loadDoc(d)
				}
				g.addTab(key, len(g.order))
				a.activateIn(g, key)
				a.showLoaded(g, d)
			} else {
				a.openIn(g, tab.Path, goTo{pos: &pos, folds: tab.Folds})
			}
			if tab.MarkdownPreview {
				if g.markdownPreview == nil {
					g.markdownPreview = map[string]bool{}
				}
				g.markdownPreview[tab.Path] = true
			}
		}
		active := saved.Active
		if saved.ActivePreview {
			active = markdownTab(active)
		}
		if g.has(active) {
			g.active = active
		}
		if g.has(saved.Preview) {
			g.preview = saved.Preview
		}
		if g.active != "" {
			g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == g.active })
			g.mru = append(g.mru, g.active)
		}
		if saved.Focused {
			focused = g
		}
		return n
	}
	if restored := restore(s.Layout, nil, 0); restored != nil {
		a.layout = restored
		if focused == nil {
			focused = a.groups()[0]
		}
		a.focus = focused
		if p := a.tabFile(focused, focused.active); p != "" {
			a.revealInTree(p)
		}
	}
}

// diffTabSpec is what of a Git tab the session keeps, nil for one it
// cannot read again.
func diffTabSpec(v *gitDiffTab) *config.DiffTab {
	switch {
	case v == nil || v.localHistory != nil || v.log != nil || v.branch != nil || v.revision != nil || v.conflict != nil || v.search != nil || v.cli:
		return nil
	case v.commit != nil || v.rev != "":
		if v.hash == "" {
			return nil
		}
		spec := &config.DiffTab{Commit: v.hash}
		if v.commit == nil {
			spec.Path, spec.OldPath = v.name, v.oldPath
		}
		return spec
	}
	return &config.DiffTab{Path: v.name, Staged: v.staged, Untracked: v.untracked}
}

// restoreDiffTab puts a Git tab of the last session in g, empty until
// the repository is read (loadPendingTabs).
func (a *App) restoreDiffTab(g *group, spec config.DiffTab) {
	if spec.Commit != "" && !isHash(spec.Commit) || spec.Path != "" && !relGitPath(spec.Path) || spec.OldPath != "" && !relGitPath(spec.OldPath) {
		return
	}
	var key string
	v := &gitDiffTab{name: spec.Path, staged: spec.Staged, untracked: spec.Untracked, hash: spec.Commit, oldPath: spec.OldPath, pending: true}
	switch {
	case spec.Commit != "" && spec.Path == "":
		key = commitTab(spec.Commit)
		v.rev, v.name = spec.Commit[:7], "Commit "+spec.Commit[:7]
		v.commit = &commitView{commit: git.Commit{Hash: spec.Commit, Short: v.rev, Subject: v.name}, loading: true}
	case spec.Commit != "":
		key, v.rev = commitFileTab(spec.Commit, spec.Path), spec.Commit[:7]
	case spec.Path != "":
		key = diffTab(spec.Path, spec.Staged)
		v.change = true
	default:
		return
	}
	if g.has(key) {
		return
	}
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	g.diffs[key] = v
	g.addTab(key, len(g.order))
	a.git.pendingTabs = append(a.git.pendingTabs, pendingTab{key: key, v: v})
}

func isHash(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// relGitPath reports whether p is a path in a repository, as git writes
// one: relative, with slashes, and not out of it.
func relGitPath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !filepath.IsAbs(p) && !slices.Contains(strings.Split(p, "/"), "..")
}

// pendingTab is a Git tab of the last session waiting to be read.
type pendingTab struct {
	key string
	v   *gitDiffTab
}

// loadPendingTabs reads the Git tabs of the last session once the
// repository is known. A change that is no longer there, or a commit no
// longer in the repository, closes its tab quietly; without a
// repository, they all close.
func (a *App) loadPendingTabs(repo bool) {
	pending := a.git.pendingTabs
	a.git.pendingTabs = nil
	for _, p := range pending {
		if !repo {
			a.dropPendingTab(p, "no Git repository")
			continue
		}
		v := p.v
		gen, scopeSeq, ctx, root := a.gen, a.git.scopeSeq, a.wsCtx, a.git.root
		change := git.Change{Path: v.name, Index: ' ', Work: ' '}
		if v.untracked {
			change.Index = '?'
		}
		for _, ch := range a.git.changes {
			if ch.Path == v.name {
				change = ch
				break
			}
		}
		go func() {
			var (
				cm      git.Commit
				details git.CommitDetails
				out     []byte
				view    gitChangeView
				err     error
			)
			switch {
			case v.commit != nil:
				if cm, err = git.ReadCommit(ctx, root, v.hash); err == nil {
					details, err = git.Show(ctx, root, v.hash)
				}
			case v.hash != "":
				out, err = git.CommitFileDiff(ctx, root, v.hash, git.FileStat{Path: v.name, OldPath: v.oldPath})
			default:
				view, err = readGitChangeView(ctx, root, change, v.staged)
				if err == nil && view.file.OldName == "" && view.file.NewName == "" {
					err = errors.New("no text diff")
				}
			}
			var patches []editor.FilePatch
			if err == nil && v.hash != "" && v.commit == nil {
				patches, err = editor.ParsePatch(string(out))
				if err == nil && (len(patches) == 0 || patches[0].Binary) {
					err = errors.New("no text diff")
				}
			}
			a.post.Post(func() {
				if a.gen != gen || a.git.scopeSeq != scopeSeq {
					return
				}
				if err != nil {
					a.dropPendingTab(p, gitErrLine(err))
					return
				}
				v.pending = false
				if v.commit != nil {
					v.name = cm.Subject
					v.commit.commit, v.commit.details, v.commit.loading = cm, details, false
				} else if v.change {
					a.setGitChangeView(v, view, 0)
				} else {
					nv := a.newGitDiffTab(patches[0], v.name, v.staged)
					v.state, v.patch = nv.state, nv.patch
				}
				for _, g := range a.groups() {
					if g == a.focus && g.diffs[p.key] == v && g.active == p.key {
						if f := a.tabFile(g, p.key); f != "" {
							a.revealInTree(f)
						}
					}
				}
			})
		}()
	}
}

// dropPendingTab closes a Git tab of the last session that cannot be
// read again.
func (a *App) dropPendingTab(p pendingTab, why string) {
	a.logf("session: dropping %q: %s", strings.TrimPrefix(p.key, diffTabPrefix), why)
	for _, g := range a.groups() {
		if g.diffs[p.key] == p.v {
			a.closeTab(g, p.key)
		}
	}
}
