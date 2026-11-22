package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"see/locales"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/filekind"
	"see/internal/git"
	"see/internal/workspace"
)

type gitState struct {
	root, branch, message  string
	head                   string // HEAD's commit, so committed versions are read again only when it moves
	refs                   string
	changes                []git.Change
	selected               map[string]bool // multi-selected changes, keyed by staged:path
	selectionAnchor        string
	decor                  map[string]byte
	err                    string
	loading, pending, busy bool
	merging                bool
	diffSeq                uint64
	resolvingPath          string
	peek                   *gitPeek
	picker                 branchPicker
	branchDiffMode         branchDiffMode
	branchPanel            *branchDiffView
	branchFingerprint      string
	// changesClosed and historyClosed are the navigator's sections
	// folded away, as VS Code's views; both show by default.
	changesClosed, historyClosed bool
	split                        float32 // the changes' share of the height while both show, as a percentage
	conflictSplit                float32
	logSplit                     float32 // the commits' share of the log's tab, as a percentage
	// upstream is the branch HEAD's tracks, and ahead and behind how
	// far apart they are; remotes are the repository's, origin first.
	upstream      string
	ahead, behind int
	remotes       []string
	web           string // the web page of the repository, or ""
	syncing       string // what talks to the remote now: Fetching, Pulling, Pushing
	autoFetch     bool
	autoFetching  bool
	lastFetch     time.Time
	// notifiedBehind is the incoming count a background fetch last
	// announced, so the next one tells only when it grew; fetchNotified
	// marks the status read following one.
	notifiedBehind int
	fetchNotified  bool
	// counts are the lines each change adds and deletes, staged and not,
	// read while the Git navigator shows.
	counts [2]map[string]git.FileStat
	// pendingTabs are the Git tabs of the last session, read once the
	// repository is known.
	pendingTabs []pendingTab
	history     gitHistory
	// kindOpen are the change groups opened in the navigator; tests,
	// docs and generated files start folded, as Peek's, and a click on
	// the group's name opens it.
	kindOpen map[filekind.Kind]bool
}

type gitPeek struct {
	doc    *doc
	group  *group
	line   int
	diff   *editor.DiffState
	scroll ui.ScrollState
}

type gitDiffTab struct {
	state        *editor.DiffState // nil for a commit
	patch        editor.FilePatch
	name         string
	staged       bool
	cli          bool        // `see --diff`'s: it compares paths, not repository changes
	rev          string      // the commit's short hash, for a file of a commit
	commit       *commitView // a commit, which shows in place of a diff
	log          *gitLogView // the log, which shows in place of a diff
	branch       *branchDiffView
	localHistory *localHistoryView
	conflict     *gitConflictView
	// What the tab shows, to read it again in the next session: the
	// commit's full hash, a renamed file's old path, and whether the
	// change is of a file git does not track.
	hash      string
	oldPath   string
	untracked bool
	pending   bool // restored, and waiting to be read
}

// label tells what a diff compares: the commit, the index or the
// working tree.
func (v *gitDiffTab) label() string {
	switch {
	case v.conflict != nil:
		return gitConflictText("gitConflictTab")
	case v.localHistory != nil:
		return localHistoryText("localHistoryTab")
	case v.cli:
		return "Diff"
	case v.rev != "":
		return v.rev
	case v.staged:
		return "Staged"
	}
	return "Working Tree"
}

// page reports whether the tab shows a commit or the log, not a file's
// diff.
func (v *gitDiffTab) page() bool { return v.commit != nil || v.log != nil || v.branch != nil }

// diffStates are the diffs the tab shows: its own, or the log's of the
// commit chosen.
func (v *gitDiffTab) diffStates() []*editor.DiffState {
	switch {
	case v.state != nil:
		return []*editor.DiffState{v.state}
	case v.log != nil && v.log.detail != nil:
		var out []*editor.DiffState
		for _, f := range v.log.detail.files {
			if f.diff != nil {
				out = append(out, f.diff)
			}
		}
		return out
	case v.branch != nil:
		var out []*editor.DiffState
		for _, f := range v.branch.files {
			if f.diff != nil {
				out = append(out, f.diff)
			}
		}
		return out
	}
	return nil
}

func (a *App) newGitDiffTab(p editor.FilePatch, name string, staged bool) *gitDiffTab {
	d := &editor.DiffState{Theme: a.edTheme}
	a.applyDiffPrefs(d)
	setPatch(d, p)
	d.GoToHunk(0)
	return &gitDiffTab{state: d, patch: p, name: name, staged: staged}
}

// setPatch shows p in d. A patch holding both of the files whole, as
// the ones made with --unified=max are, becomes a diff of them whose
// unchanged stretches collapse; any other is shown hunk by hunk.
func setPatch(d *editor.DiffState, p editor.FilePatch) {
	if old, nw, ok := patchFiles(p); ok && d.SetPatchFiles(p, old, nw) == nil {
		return
	}
	d.SetPatch(p)
}

// patchFiles rebuilds the texts of a patch holding one hunk starting
// at the files' first line — the shape a --unified=max diff has, and
// the only one that proves it leaves nothing out.
func patchFiles(p editor.FilePatch) (old, new string, ok bool) {
	if len(p.Hunks) != 1 || p.Binary {
		return "", "", false
	}
	h := p.Hunks[0]
	if h.OldStart > 1 || h.NewStart > 1 {
		return "", "", false
	}
	var oldB, newB strings.Builder
	oldLine, newLine := 0, 0
	prev := byte(0)
	oldEOL, newEOL := true, true
	for _, l := range h.Lines {
		if l != "" && l[0] == '\\' {
			oldEOL = oldEOL && prev != '-' && prev != ' '
			newEOL = newEOL && prev != '+' && prev != ' '
			continue
		}
		if l == "" {
			continue
		}
		prev = l[0]
		text := strings.TrimSuffix(l[1:], "\r")
		if l[0] != '+' {
			if oldLine > 0 {
				oldB.WriteByte('\n')
			}
			oldB.WriteString(text)
			oldLine++
		}
		if l[0] != '-' {
			if newLine > 0 {
				newB.WriteByte('\n')
			}
			newB.WriteString(text)
			newLine++
		}
	}
	if oldEOL && oldLine > 0 {
		oldB.WriteByte('\n')
	}
	if newEOL && newLine > 0 {
		newB.WriteByte('\n')
	}
	return oldB.String(), newB.String(), true
}

func (a *App) startGit() {
	if watchFiles == nil {
		return
	}
	a.git.autoFetch = true
	a.refreshGit()
	ctx, gen := a.wsCtx, a.gen
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.post.Post(func() {
					if a.gen == gen {
						a.refreshGit()
					}
				})
			}
		}
	}()
}

func (a *App) refreshGit() {
	if a.root == "" || a.wsCtx == nil {
		return
	}
	if a.git.loading || a.git.busy && !a.git.autoFetching {
		a.git.pending = true
		return
	}
	a.git.loading = true
	ctx, root, gen := a.wsCtx, a.root, a.gen
	counting := a.navigator == navGit && !a.sidebarOff
	go func() {
		// One status --porcelain=v2 --branch names HEAD, the branch and
		// what it tracks as well as the changes; for-each-ref and remote
		// -v are the only other calls, plus the diffs the counts ask for.
		s, err := git.Sync(ctx, root, counting)
		branchFingerprint := gitBranchFingerprint(ctx, s.Root, s.Head, s.Refs, s.Changes)
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			a.git.loading = false
			if a.git.root != s.Root || a.git.branch != s.Branch || a.git.head != s.Head || a.git.upstream != s.Upstream || s.RefsErr == nil && a.git.refs != s.Refs {
				a.git.history.stale = true
			}
			if s.RefsErr == nil {
				a.git.refs = s.Refs
			}
			a.git.root, a.git.branch, a.git.changes, a.git.head = s.Root, s.Branch, s.Changes, s.Head
			a.git.merging = s.Merging
			a.git.branchFingerprint = branchFingerprint
			a.git.pruneSelection()
			a.git.upstream, a.git.ahead, a.git.behind = s.Upstream, s.Ahead, s.Behind
			if s.Behind < a.git.notifiedBehind {
				a.git.notifiedBehind = s.Behind
			}
			if a.git.fetchNotified {
				a.git.fetchNotified = false
				a.notifyIncoming(s.Behind)
			}
			if counting || len(s.Changes) == 0 {
				a.git.counts = [2]map[string]git.FileStat{s.Staged, s.Unstaged}
			}
			if err == nil {
				a.git.web, a.git.remotes = s.Web, s.Remotes
			}
			a.git.decor = git.Decorations(s.Root, s.Changes)
			for _, d := range a.docs {
				if d.buf != nil {
					a.loadGitBase(d)
					a.syncBlame(d)
				}
			}
			a.git.err = ""
			if err != nil {
				a.git.err = err.Error()
			}
			if len(a.git.pendingTabs) > 0 {
				a.loadPendingTabs(err == nil)
			}
			if a.git.pending {
				a.git.pending = false
				a.refreshGit()
			}
			a.maybeFetchGit(time.Now())
			a.refreshBranchDiffs()
		})
	}()
}

func gitBranchFingerprint(ctx context.Context, root, head, refs string, changes []git.Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00%s", root, head, refs)
	if root == "" {
		return b.String()
	}
	appendStat := func(path string) {
		if st, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, "\x00%s:%d:%d", path, st.Size(), st.ModTime().UnixNano())
		} else {
			fmt.Fprintf(&b, "\x00%s:missing", path)
		}
	}
	index, err := git.Command(ctx, root, "rev-parse", "--git-path", "index")
	if err != nil {
		b.WriteString("\x00index:error")
	} else {
		indexPath := strings.TrimSpace(string(index))
		if !filepath.IsAbs(indexPath) {
			indexPath = filepath.Join(root, indexPath)
		}
		appendStat(indexPath)
	}
	for _, ch := range changes {
		fmt.Fprintf(&b, "\x00%c%c:%s:%s", ch.Index, ch.Work, ch.Path, ch.OldPath)
		for _, name := range []string{ch.Path, ch.OldPath} {
			if name != "" {
				appendStat(filepath.Join(root, filepath.FromSlash(name)))
			}
		}
	}
	return b.String()
}

// loadGitBase reads the committed version off the UI thread. A tracked file
// added to the index has an empty committed version. It is read again only
// when the repository, HEAD, the file's path or its status changed: git's
// status refreshes every few seconds, and reading every open file's
// committed version each time spawned two git processes a file.
func (a *App) loadGitBase(d *doc) {
	if key := a.gitBaseKey(d); key != "" && key == d.gitBaseKey {
		return
	}
	d.gitBaseSeq++
	d.gitBaseKey = ""
	seq, gen, root, ctx, path := d.gitBaseSeq, a.gen, a.git.root, a.wsCtx, d.path
	if root == "" || ctx == nil || d.untitled {
		return
	}
	key := a.gitBaseKey(d)
	gitPath := path
	if !workspace.Within(root, gitPath) && a.realRoot != "" && workspace.Within(a.root, path) {
		if sub, err := filepath.Rel(a.root, path); err == nil {
			gitPath = filepath.Join(a.realRoot, sub)
		}
	}
	if !workspace.Within(root, gitPath) {
		return
	}
	rel, err := filepath.Rel(root, gitPath)
	if err != nil {
		return
	}
	d.gitBaseKey = key
	go func() {
		_, trackedErr := git.Command(ctx, root, "ls-files", "--error-unmatch", "--", rel)
		var base []byte
		if trackedErr == nil {
			base, err = git.Command(ctx, root, "show", "HEAD:"+filepath.ToSlash(rel))
		}
		// A file newly added to the index has no committed contents.
		if err != nil {
			base = nil
		}
		valid := trackedErr == nil && len(base) <= 4<<20 && utf8.Valid(base) && !strings.ContainsRune(string(base), 0)
		a.post.Post(func() {
			if a.gen != gen || a.docs[path] != d || d.gitBaseSeq != seq {
				return
			}
			if trackedErr != nil || err != nil {
				// Untracked, newly added or a failed read: ask again on
				// the next refresh rather than trusting it.
				d.gitBaseKey = ""
			}
			if valid && d.gitBase != nil && d.gitBase.String() == string(base) || !valid && d.gitBase == nil {
				return
			}
			d.gitBase = nil
			d.gitChanges = nil
			if valid {
				d.gitBase = editor.NewBuffer(string(base))
				d.gitChanges = &editor.DiffState{Old: d.gitBase, New: d.buf}
			}
			for _, g := range a.groups() {
				if st := g.eds[path]; st != nil {
					st.DiffBase = d.gitBase
				}
			}
			if a.git.peek != nil && a.git.peek.doc == d {
				a.git.peek = nil
			}
		})
	}()
}

// gitBaseKey names what d's committed version depends on, or "" when it
// cannot be told.
func (a *App) gitBaseKey(d *doc) string {
	root := a.git.root
	if root == "" || a.git.head == "" || d.untitled {
		return ""
	}
	rel, ok := a.gitRelPath(d.path)
	if !ok {
		return ""
	}
	status := "  "
	for _, ch := range a.git.changes {
		if ch.Path == rel || ch.OldPath == rel {
			status = string([]byte{ch.Index, ch.Work})
			break
		}
	}
	return root + "\x00" + a.git.head + "\x00" + rel + "\x00" + status
}

func (a *App) showGitPeek(g *group, d *doc, st *editor.State, line int) {
	if st.DiffBase != nil {
		diff := &editor.DiffState{Old: st.DiffBase, New: d.buf, Layout: editor.DiffUnified, Theme: a.edTheme, FitContent: true}
		a.applyDiffPrefs(diff)
		for i, h := range diff.Hunks() {
			if line >= h.NewStart && line < max(h.NewEnd, h.NewStart+1) || h.NewStart >= d.buf.LineCount() && line == d.buf.LineCount()-1 {
				a.choices = nil
				a.peek = nil
				diff.GoToHunk(i)
				a.git.peek = &gitPeek{doc: d, group: g, line: line, diff: diff}
				a.git.peek.scroll.Y = diffHunkOffset(st, diff, i)
				return
			}
		}
	}
	a.git.peek = nil
	start := d.buf.Offset(editor.Pos{Line: line})
	end := d.buf.Len()
	if line+1 < d.buf.LineCount() {
		end = d.buf.Offset(editor.Pos{Line: line + 1})
	}
	st.Select(start, end)
}

func (a *App) gitStatusColor(status byte) ui.Color {
	if a.edTheme == nil {
		return a.colors().Muted
	}
	switch status {
	case 'U', 'A':
		return a.edTheme.Added
	case 'D', '!':
		return a.edTheme.Deleted
	default:
		return a.edTheme.Modified
	}
}

func (a *App) gitAction(args ...string) {
	if a.git.busy || a.git.root == "" {
		return
	}
	a.git.busy = true
	ctx, root, gen := a.wsCtx, a.git.root, a.gen
	go func() {
		_, err := git.Command(ctx, root, args...)
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			a.git.busy = false
			if err != nil {
				a.note(locales.MsgGit()+err.Error(), true)
			} else {
				a.git.diffSeq++
				if args[0] == "commit" {
					a.git.message = ""
					a.note(locales.MsgCommittedStagedChanges(), false)
				}
			}
			a.refreshGit()
		})
	}()
}

func (a *App) openGitFile(ch git.Change) {
	path := filepath.Join(a.git.root, filepath.FromSlash(ch.Path))
	a.term.maximized = false
	a.git.diffSeq++
	a.userOpen(path)
}

func (a *App) discardGitChange(ch git.Change) {
	if a.git.busy || a.git.root == "" || a.deleteConfirm != nil {
		return
	}
	path := workspace.DocPath(a.root, a.realRoot, filepath.Join(a.git.root, filepath.FromSlash(ch.Path)))
	if d := a.docs[path]; d != nil && d.buf != nil && d.buf.Modified() {
		a.note(locales.MsgSaveOrCloseTheFileSUnsavedEdits(), true)
		return
	}
	message := locales.DiscardFileQuestion(ch.Path)
	if ch.Index == '?' {
		message = locales.TrashUntrackedQuestion(ch.Path)
	}
	q := &confirmation{message: message}
	q.actions = []action{
		{locales.DiscardChanges(), func() {
			if a.deleteConfirm != q || a.git.busy {
				return
			}
			a.deleteConfirm = nil
			if d := a.docs[path]; d != nil && d.buf != nil && d.buf.Modified() {
				a.note(locales.MsgSaveOrCloseTheFileSUnsavedEdits(), true)
				return
			}
			a.git.busy = true
			ctx, root, gen, trash := a.wsCtx, a.git.root, a.gen, a.host.TrashItem
			go func() {
				var err error
				if ch.Index == '?' {
					err = trash(path)
				} else {
					_, err = git.Command(ctx, root, "restore", "--worktree", "--", ch.Path)
				}
				a.post.Post(func() {
					if a.gen != gen {
						return
					}
					a.git.busy = false
					if err != nil {
						a.note(locales.MsgCouldNotDiscardGitChanges()+err.Error(), true)
					} else {
						a.git.diffSeq++
						if ch.Index == '?' {
							a.removed(path)
						} else if d := a.docs[path]; d != nil {
							a.reloadDoc(d)
						}
						a.refreshDir(filepath.Dir(path))
						a.quick.files = nil
						a.filter.forQuery = ""
					}
					a.refreshGit()
				})
			}()
		}},
		{locales.Cancel(), func() {
			if a.deleteConfirm == q {
				a.deleteConfirm = nil
			}
		}},
	}
	a.deleteConfirm = q
}

func (a *App) discardGitChanges(chs []git.Change) {
	if a.git.busy || a.git.root == "" || a.deleteConfirm != nil {
		return
	}
	for _, ch := range chs {
		path := workspace.DocPath(a.root, a.realRoot, filepath.Join(a.git.root, filepath.FromSlash(ch.Path)))
		if d := a.docs[path]; d != nil && d.buf != nil && d.buf.Modified() {
			a.note(locales.MsgSaveOrCloseTheFileSUnsavedEdits(), true)
			return
		}
	}
	message := locales.DiscardFilesQuestion(len(chs))
	q := &confirmation{message: message}
	q.actions = []action{
		{locales.DiscardChanges(), func() {
			if a.deleteConfirm != q || a.git.busy {
				return
			}
			a.deleteConfirm = nil
			a.git.busy = true
			ctx, root, gen, trash := a.wsCtx, a.git.root, a.gen, a.host.TrashItem
			go func() {
				var firstErr error
				for _, ch := range chs {
					path := workspace.DocPath(a.root, a.realRoot, filepath.Join(root, filepath.FromSlash(ch.Path)))
					var err error
					if ch.Index == '?' {
						err = trash(path)
					} else {
						_, err = git.Command(ctx, root, "restore", "--worktree", "--", ch.Path)
					}
					if err != nil && firstErr == nil {
						firstErr = err
					}
					a.post.Post(func() {
						if ch.Index == '?' {
							if err == nil {
								a.removed(path)
							}
						} else if d := a.docs[path]; d != nil && err == nil {
							a.reloadDoc(d)
						}
						if err == nil {
							a.refreshDir(filepath.Dir(path))
						}
					})
				}
				a.post.Post(func() {
					if a.gen != gen {
						return
					}
					a.git.busy = false
					if firstErr != nil {
						a.note(locales.MsgCouldNotDiscardGitChanges()+firstErr.Error(), true)
					} else {
						a.git.diffSeq++
						a.quick.files = nil
						a.filter.forQuery = ""
					}
					a.refreshGit()
				})
			}()
		}},
		{locales.Cancel(), func() {
			if a.deleteConfirm == q {
				a.deleteConfirm = nil
			}
		}},
	}
	a.deleteConfirm = q
}

func gitChangeKey(staged bool, path string) string {
	return fmt.Sprintf("%t\x00%s", staged, path)
}

func (g *gitState) changeKeys() []string {
	var keys []string
	for _, staged := range []bool{true, false} {
		for _, ch := range g.changes {
			if gitChangeInSection(ch, staged) {
				keys = append(keys, gitChangeKey(staged, ch.Path))
			}
		}
	}
	return keys
}

func gitChangeInSection(ch git.Change, staged bool) bool {
	if ch.Conflicted() {
		return false
	}
	if staged {
		return ch.Index != ' ' && ch.Index != '?'
	}
	return ch.Work != ' ' || ch.Index == '?'
}

func (g *gitState) pruneSelection() {
	valid := make(map[string]bool)
	for _, key := range g.changeKeys() {
		valid[key] = true
	}
	for key := range g.selected {
		if !valid[key] {
			delete(g.selected, key)
		}
	}
	if !valid[g.selectionAnchor] {
		g.selectionAnchor = ""
	}
}

func (g *gitState) selectChange(key string, mods ui.Modifiers) {
	if mods&ui.Shift != 0 && g.selectionAnchor != "" {
		keys := g.changeKeys()
		start, end := -1, -1
		for i, item := range keys {
			if item == g.selectionAnchor {
				start = i
			}
			if item == key {
				end = i
			}
		}
		if start >= 0 && end >= 0 {
			if mods&(ui.Cmd|ui.Ctrl) == 0 {
				g.selected = make(map[string]bool)
			}
			if g.selected == nil {
				g.selected = make(map[string]bool)
			}
			if start > end {
				start, end = end, start
			}
			for _, item := range keys[start : end+1] {
				g.selected[item] = true
			}
			return
		}
	}
	if mods&(ui.Cmd|ui.Ctrl) == 0 {
		g.selected = make(map[string]bool)
	}
	if g.selected == nil {
		g.selected = make(map[string]bool)
	}
	if mods&(ui.Cmd|ui.Ctrl) != 0 && g.selected[key] {
		delete(g.selected, key)
	} else {
		g.selected[key] = true
	}
	g.selectionAnchor = key
}

func (a *App) stageGitChanges(staged bool, changes []git.Change) {
	var paths []string
	for _, ch := range changes {
		if ch.Conflicted() {
			continue
		}
		paths = append(paths, ch.Path)
		if ch.OldPath != "" {
			paths = append(paths, ch.OldPath)
		}
	}
	if len(paths) == 0 {
		return
	}
	if staged {
		a.gitAction(append([]string{"reset", "-q", "--"}, paths...)...)
	} else {
		a.gitAction(append([]string{"add", "--"}, paths...)...)
	}
}

func (a *App) stageAllGitChanges(staged bool) {
	var changes []git.Change
	for _, ch := range a.git.changes {
		if gitChangeInSection(ch, staged) {
			changes = append(changes, ch)
		}
	}
	a.stageGitChanges(staged, changes)
}

func (a *App) showGitDiff(ch git.Change, staged bool) {
	a.showGitDiffIn(ch, staged, a.focus, sideNone, len(a.focus.order))
}

func (a *App) showGitDiffIn(ch git.Change, staged bool, target *group, placement side, at int) {
	if a.git.root == "" {
		return
	}
	a.term.maximized = false
	a.git.peek = nil
	a.git.diffSeq++
	if ch.Conflicted() {
		a.openGitConflictIn(ch, target, placement, at)
		return
	}
	seq, gen, root, ctx := a.git.diffSeq, a.gen, a.git.root, a.wsCtx
	go func() {
		out, err := changeDiff(ctx, root, ch.Path, staged, ch.Index == '?')
		patches, parseErr := editor.ParsePatch(string(out))
		a.post.Post(func() {
			if a.gen != gen || a.git.diffSeq != seq || a.layout.find(target) == nil {
				return
			}
			if err != nil {
				a.note(locales.MsgGitDiff()+err.Error(), true)
				return
			}
			if parseErr != nil || len(patches) == 0 || patches[0].Binary {
				a.note(locales.MsgNoTextDiffAvailableFor()+ch.Path, true)
				return
			}
			v := a.newGitDiffTab(patches[0], ch.Path, staged)
			v.untracked = ch.Index == '?'
			key := diffTab(ch.Path, staged)
			if placement != sideNone {
				target = a.splitBeside(target, placement)
				at = 0
			}
			if target.diffs == nil {
				target.diffs = map[string]*gitDiffTab{}
			}
			target.diffs[key] = v
			target.addTab(key, at)
			a.activateIn(target, key)
		})
	}()
}

func (a *App) gitNavigator(c *ui.Context) {
	if a.git.root == "" {
		msg := "Looking for a Git repository…"
		if a.git.err != "" {
			msg = "No Git repository found, or Git is unavailable."
		}
		ui.Text(c, msg).TextColor(colors(c).Muted).FontSize(12).Padding(12, 10).Tooltip(a.git.err)
		return
	}
	staged, unstaged, conflicts := 0, 0, 0
	for _, ch := range a.git.changes {
		if ch.Conflicted() {
			conflicts++
		}
		if gitChangeInSection(ch, true) {
			staged++
		}
		if gitChangeInSection(ch, false) {
			unstaged++
		}
	}
	// The changes over the history, each a section that folds, as VS
	// Code's views; while both show, a divider between them sets how
	// much of the navigator each takes.
	changes := func() {
		if a.git.branchDiffMode == compareBaseMode {
			count := 0
			if a.git.branchPanel != nil {
				count = len(a.git.branchPanel.files)
			}
			a.gitSectionHeader(c, "Changes", count, &a.git.changesClosed, func() {})
			if a.git.changesClosed {
				return
			}
			a.branchDiffModePicker(c)
			if conflicts > 0 {
				label := fmt.Sprintf("%s (%d)", gitConflictText("gitConflictedFiles"), conflicts)
				ui.Row(c).Padding(2, sidebarInset).Children(func() {
					ui.Button(c, label).FontSize(11).OnClick(func() { a.git.branchDiffMode = uncommittedMode })
				})
			}
			a.branchDiffNavigator(c)
			return
		}
		a.gitChangesHeader(c)
		if a.git.changesClosed {
			return
		}
		a.branchDiffModePicker(c)
		a.commitBar(c, staged, conflicts > 0)
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 0, 6, 0).Children(func() {
				if len(a.git.changes) == 0 {
					ui.Text(c, locales.MsgNoChanges()).FontSize(12).TextColor(colors(c).Muted).Padding(4, 16, 8, 16)
				}
				if conflicts > 0 {
					a.gitSection(c, gitConflictText("gitConflictedFiles"), conflicts)
					for _, ch := range a.git.changes {
						if ch.Conflicted() {
							a.gitConflictRow(c, ch)
						}
					}
				}
				if staged > 0 {
					a.gitSection(c, "Staged Changes", staged)
				}
				a.gitRowsByKind(c, true)
				if unstaged > 0 && (staged > 0 || conflicts > 0) {
					a.gitSection(c, "Changes", unstaged)
				}
				a.gitRowsByKind(c, false)
			})
		})
	}
	history := func() {
		a.gitHistoryHeader(c)
		if !a.git.historyClosed {
			a.historyNavigator(c)
		}
	}
	if conflicts == 0 {
		a.git.conflictSplit = 0
	}
	switch {
	case !a.git.changesClosed && !a.git.historyClosed:
		if a.git.split <= 0 {
			a.git.split = 40
		}
		split := &a.git.split
		if conflicts > 0 {
			if a.git.conflictSplit <= 0 {
				a.git.conflictSplit = max(a.git.split, 62)
			}
			split = &a.git.conflictSplit
		}
		ui.SplitVerticalPercent(c, split,
			func() { ui.Column(c).Fill().Children(changes) },
			func() { ui.Column(c).Fill().Children(history) }).Grow(1).FillWidth()
	case a.git.historyClosed:
		// The history's head at the navigator's foot, under the changes.
		ui.Column(c).Grow(1).FillWidth().Children(changes)
		history()
	default:
		changes()
		ui.Column(c).Grow(1).FillWidth().Children(history)
	}
}

// gitRowsByKind lists the staged or unstaged changes grouped by what
// they hold: code rows flat, then tests, docs and generated files under
// headers that fold, opened by a click.
func (a *App) gitRowsByKind(c *ui.Context, staged bool) {
	if a.git.kindOpen == nil {
		a.git.kindOpen = map[filekind.Kind]bool{}
	}
	groups := map[filekind.Kind][]git.Change{}
	for _, ch := range a.git.changes {
		if gitChangeInSection(ch, staged) {
			k := filekind.Of(filepath.ToSlash(ch.Path))
			groups[k] = append(groups[k], ch)
		}
	}
	for _, ch := range groups[filekind.Code] {
		a.gitRow(c, ch, staged)
	}
	for _, k := range []filekind.Kind{filekind.Test, filekind.Docs, filekind.Generated} {
		rows := groups[k]
		if len(rows) == 0 {
			continue
		}
		open := a.git.kindOpen[k]
		a.gitKindHeader(c, k, len(rows), open, func() { a.git.kindOpen[k] = !open })
		if !open {
			continue
		}
		for _, ch := range rows {
			a.gitRow(c, ch, staged)
		}
	}
}

// kindName is the change group's name in the window's language.
func kindName(k filekind.Kind) string {
	switch k {
	case filekind.Test:
		return logText("gitKindTests")
	case filekind.Docs:
		return logText("gitKindDocs")
	case filekind.Generated:
		return logText("gitKindGenerated")
	}
	return filekind.Names[k]
}

// gitKindHeader is a change group's name: an arrow, the name and the
// count, which a click folds or unfolds.
func (a *App) gitKindHeader(c *ui.Context, k filekind.Kind, count int, open bool, toggle func()) {
	row := ui.Row(c.Key("kind:"+fmt.Sprint(int(k)))).FillWidth().Padding(9, 16, 4, 16).Gap(5).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Label(kindName(k))
	row.OnClick(toggle)
	row.Children(func() {
		turn := float32(90)
		if !open {
			turn = 0
		}
		symbolIcon(c, icChevron, 9, colors(c).Dim).Rotate(turn)
		ui.Text(c, kindName(k)).FontSize(11).Bold().TextColor(colors(c).Muted)
		ui.Text(c, fmt.Sprint(count)).FontSize(11).TextColor(colors(c).Dim)
	})
}

func (a *App) gitSection(c *ui.Context, name string, count int) {
	ui.Row(c).FillWidth().Padding(9, 16, 4, 16).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, name).FontSize(11).Bold().TextColor(colors(c).Muted)
		ui.Text(c, fmt.Sprint(count)).FontSize(11).TextColor(colors(c).Dim)
	})
}

func (a *App) gitRow(c *ui.Context, ch git.Change, staged bool) {
	label, action := "Unstaged", "Stage"
	if staged {
		label, action = "Staged", "Unstage"
	}
	container := ui.Row(c.Key(fmt.Sprintf("%t:%s", staged, ch.Path))).FillWidth().Padding(1, 13).Gap(2).Radius(4).AlignItems(ui.Center)
	hovered := container.Hovered()
	if a.git.selected[gitChangeKey(staged, ch.Path)] {
		container.Background(colors(c).SelectedRow)
	} else if len(a.git.selected) == 0 && a.focus.active == diffTab(ch.Path, staged) {
		container.Background(colors(c).SelectedRow)
	} else if hovered {
		container.Background(colors(c).Pill)
	}
	container.Children(func() {
		row := ui.ButtonBase(c).Label(label+locales.MsgDiff()+ch.Path).Tooltip(ch.Path).Grow(1).Padding(4, 4).Radius(4)
		row.Drag(&tabDrag{change: &ch, staged: staged})
		row.Children(func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				fileIcon(c, filepath.Base(ch.Path), 15)
				ui.Text(c, filepath.Base(ch.Path)).FontSize(12).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
				dir := filepath.Dir(ch.Path)
				if dir != "." {
					ui.Text(c, filepath.ToSlash(dir)).FontSize(11).TextColor(colors(c).Dim).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
				}
			})
		})
		mods := row.ClickModifiers()
		row.OnClick(func() {
			a.git.selectChange(gitChangeKey(staged, ch.Path), mods)
			if mods&(ui.Cmd|ui.Ctrl|ui.Shift) == 0 {
				a.showGitDiff(ch, staged)
			}
		})
		if hovered {
			if ch.Work != 'D' && !(ch.Index == 'D' && ch.Work == ' ') && glyphButton(c, icGitOpen, "Open file "+ch.Path, "Open File", false, false).Clicked() {
				a.openGitFile(ch)
			}
			if !staged && glyphButton(c, icGitDiscard, "Discard changes "+ch.Path, "Discard Changes", false, a.git.busy).Clicked() {
				if a.git.selected[gitChangeKey(false, ch.Path)] {
					var targets []git.Change
					for _, c2 := range a.git.changes {
						if a.git.selected[gitChangeKey(false, c2.Path)] {
							targets = append(targets, c2)
						}
					}
					seen := map[string]bool{}
					var uniq []git.Change
					for _, t := range targets {
						if seen[t.Path] {
							continue
						}
						seen[t.Path] = true
						uniq = append(uniq, t)
					}
					if len(uniq) > 1 {
						a.discardGitChanges(uniq)
						return
					}
				}
				a.discardGitChange(ch)
			}
			icon := icPlus
			if staged {
				icon = icMinus
			}
			if glyphButton(c, icon, action+" "+ch.Path, action, false, a.git.busy).Clicked() {
				targets := []git.Change{ch}
				if a.git.selected[gitChangeKey(staged, ch.Path)] {
					for _, c2 := range a.git.changes {
						if a.git.selected[gitChangeKey(staged, c2.Path)] {
							targets = append(targets, c2)
						}
					}
					// de-duplicate
					seen := map[string]bool{}
					var uniq []git.Change
					for _, t := range targets {
						if seen[t.Path] {
							continue
						}
						seen[t.Path] = true
						uniq = append(uniq, t)
					}
					targets = uniq
				}
				a.stageGitChanges(staged, targets)
			}
		}
		status := ch.Work
		if staged {
			status = ch.Index
		}
		if status == '?' {
			status = 'U'
		}
		if !hovered {
			a.lineCounts(c, ch.Path, staged)
		}
		ui.Text(c, string(status)).FontSize(11).TextColor(a.gitStatusColor(status)).Padding(0, 4)
	})
}

// lineCounts shows the lines a change adds and deletes, as Peek's list.
func (a *App) lineCounts(c *ui.Context, path string, staged bool) {
	m := a.git.counts[1]
	if staged {
		m = a.git.counts[0]
	}
	f, ok := m[path]
	if !ok || f.Added < 0 {
		return
	}
	ui.Row(c).Gap(4).Shrink(0).Children(func() {
		if f.Added > 0 {
			ui.Text(c, fmt.Sprintf("+%d", f.Added)).FontSize(10).TextColor(a.gitStatusColor('A')).SingleLine()
		}
		if f.Deleted > 0 {
			ui.Text(c, fmt.Sprintf("−%d", f.Deleted)).FontSize(10).TextColor(a.gitStatusColor('D')).SingleLine()
		}
	})
}

// commitBar is the field over the Git navigator's changes, as the
// project navigator's filter: a commit message, Return commits what is
// staged, and its button at the end does the same.
func (a *App) commitBar(c *ui.Context, staged int, conflicted bool) {
	can := !a.git.busy && !conflicted && (staged > 0 || a.git.merging) && strings.TrimSpace(a.git.message) != ""
	commit := func() {
		if can {
			a.gitAction("commit", "-m", a.git.message)
		}
	}
	placeholder := "Commit Message"
	if staged == 0 && !a.git.merging {
		placeholder = "Stage changes to commit"
	}
	if conflicted {
		placeholder = gitConflictText("gitResolveBeforeCommit")
	}
	ui.Row(c).Shrink(0).FillWidth().Padding(2, sidebarInset, 6, sidebarInset).Children(func() {
		ui.Row(c).FillWidth().Height(28).Radius(14).Background(colors(c).Pill).Padding(0, 3, 0, 12).Gap(4).AlignItems(ui.Center).Children(func() {
			in := ui.TextInput(c, &a.git.message).Placeholder(placeholder).Label(locales.MsgCommitMessage()).
				FontSize(12).Grow(1).Background(ui.Transparent).BorderWidth(0).Padding(0)
			if in.Submitted() {
				commit()
			}
			tip := "Commit " + fmt.Sprint(staged) + " staged change" + plural(staged)
			if a.git.merging {
				tip = gitConflictText("gitCommitMerge")
			}
			b := ui.ButtonBase(c).Label(locales.MsgCommit()).Tooltip(tip).Size(22, 22).Radius(11).Center().Disabled(!can).KeepFocus()
			color := colors(c).Dim
			if can {
				b.Background(c.Theme().Accent)
				color = ui.RGB(0xff, 0xff, 0xff)
			}
			b.Children(func() { symbolIcon(c, icCommitArrow, 12, color) })
			b.OnClick(commit)
		})
	})
}

// changeDiff is the whole patch of a change of the working tree, or of
// the index with staged; an untracked file's is against nothing.
func changeDiff(ctx context.Context, root, path string, staged, untracked bool) ([]byte, error) {
	if untracked {
		out, err := git.Command(ctx, root, "diff", "--no-index", "--no-ext-diff", "--no-color", "--unified=2147483647", "--", "/dev/null", filepath.Join(root, path))
		if len(out) > 0 {
			err = nil // --no-index fails when there is a difference
		}
		return out, err
	}
	args := []string{"diff", "--no-ext-diff", "--no-color", "--unified=2147483647"}
	if staged {
		args = append(args, "--cached")
	}
	return git.Command(ctx, root, append(args, "--", path)...)
}
