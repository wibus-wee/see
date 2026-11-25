package app

import (
	"github.com/egoist/mygo/ui"

	"see/internal/git"
)

func (a *App) revealLogCommit(hash string) {
	if hash == "" || a.git.root == "" {
		return
	}
	a.openGitLog()
	v := a.focus.diffs[gitLogTab].log
	if v.path != "" {
		// A commit named from outside the file's history is not its,
		// so the reveal reads the repository's again.
		v.path = ""
		a.git.history.stale = true
	}
	v.query, v.pending, v.reading = "", hash, false
}

func (a *App) resolveLogTarget(v *gitLogView) {
	if v.pending == "" || v.reading {
		return
	}
	h := &a.git.history
	if h.loading {
		return
	}
	v.query = ""
	for i, cm := range h.commits {
		if cm.Hash == v.pending {
			a.chooseLogCommit(v, cm)
			v.list.ScrollTo(i, ui.Center)
			return
		}
	}
	if !h.done && h.err == "" {
		a.loadHistory(false)
		return
	}
	v.reading = true
	hash, gen, scopeSeq, root, ctx := v.pending, a.gen, a.git.scopeSeq, a.git.root, a.wsCtx
	go func() {
		cm, err := git.ReadCommit(ctx, root, hash)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq || v.pending != hash {
				return
			}
			v.reading, v.pending = false, ""
			if err != nil {
				a.note(logText("blameRevealFailed")+gitErrLine(err), true)
				return
			}
			a.chooseLogCommit(v, cm)
		})
	}()
}
