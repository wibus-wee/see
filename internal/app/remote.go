package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/git"
)

// Fetch, Pull and Push, as Xcode's Source Control menu, and how far the
// branch is from the one it tracks, as Peek counts incoming commits.

var (
	icPull  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v14M6 12l6 6 6-6"/></svg>`))
	icPush  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20V6M6 12l6-6 6 6"/></svg>`))
	icFetch = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M7 18a4.5 4.5 0 0 1-.6-9A6 6 0 0 1 18 8.5a4 4 0 0 1-.5 9.5"/><path d="M12 11v9M9 17l3 3 3-3"/></svg>`))
)

// syncGit runs a command that talks to a remote, one at a time, saying
// what it does in the toolbar.
func (a *App) syncGit(doing, done string, args ...string) {
	if a.git.busy || a.git.root == "" {
		return
	}
	a.git.busy, a.git.syncing = true, doing
	ctx, root, gen := a.wsCtx, a.git.root, a.gen
	go func() {
		_, err := git.Network(ctx, root, args...)
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			a.git.busy, a.git.syncing = false, ""
			if err != nil {
				a.note("Git: "+gitErrLine(err), true)
			} else {
				a.note(done, false)
				a.git.history.stale = true
				if args[0] == "pull" && !a.watching {
					a.refreshAll()
				}
			}
			a.refreshGit()
		})
	}()
}

func (a *App) gitFetch() { a.syncGit("Fetching…", "Fetched.", "fetch", "--all", "--prune") }

// gitPull brings the branch up to its upstream without a merge commit,
// as Peek's one-click pull: one that would need a merge says so.
func (a *App) gitPull() {
	if a.git.upstream == "" {
		a.note("The branch tracks no remote branch to pull from.", true)
		return
	}
	a.syncGit("Pulling…", "Pulled from "+a.git.upstream+".", "pull", "--ff-only")
}

// gitPush pushes the branch, to the branch it tracks, or to a new one of
// its name on the first remote, which it then tracks.
func (a *App) gitPush() {
	switch {
	case a.git.upstream != "":
		a.syncGit("Pushing…", "Pushed to "+a.git.upstream+".", "push")
	case len(a.git.remotes) == 0:
		a.note("The repository has no remote to push to.", true)
	default:
		remote := a.git.remotes[0]
		a.syncGit("Pushing…", "Pushed to "+remote+"/"+a.git.branch+".", "push", "--set-upstream", remote, "HEAD")
	}
}

// syncBar is the Git navigator's line about the remote: the branch
// tracked and how far apart, with Fetch, Pull and Push.
func (a *App) syncBar(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(0, 10, 6, 14).Gap(4).AlignItems(ui.Center).Children(func() {
		switch {
		case a.git.syncing != "":
			ui.Spinner(c).FontSize(10)
			ui.Text(c, a.git.syncing).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		case a.git.upstream != "":
			ui.Text(c, a.git.upstream).FontSize(11).TextColor(colors(c).Muted).SingleLine().Shrink(1).
				Tooltip(a.git.branch + " tracks " + a.git.upstream)
			if s := aheadBehind(a.git.ahead, a.git.behind); s != "" {
				ui.Text(c, s).FontSize(11).TextColor(colors(c).Text).SingleLine().Shrink(0)
			} else {
				ui.Text(c, "Up to date").FontSize(11).TextColor(colors(c).Dim).SingleLine().Shrink(1)
			}
		case len(a.git.remotes) > 0:
			ui.Text(c, "Not on a remote yet").FontSize(11).TextColor(colors(c).Dim).SingleLine().Shrink(1)
		default:
			ui.Text(c, "No remote").FontSize(11).TextColor(colors(c).Dim).SingleLine().Shrink(1)
		}
		ui.Box(c).Grow(1)
		noRemote := len(a.git.remotes) == 0 && a.git.upstream == ""
		if glyphButton(c, icFetch, "Fetch", "Fetch from every remote", false, a.git.busy || noRemote).Clicked() {
			a.gitFetch()
		}
		pull := "Pull"
		if a.git.behind > 0 {
			pull = fmt.Sprintf("Pull %d incoming %s", a.git.behind, "commit"+plural(a.git.behind))
		}
		if glyphButton(c, icPull, "Pull", pull, a.git.behind > 0, a.git.busy || a.git.upstream == "").Clicked() {
			a.gitPull()
		}
		push := "Push"
		if a.git.ahead > 0 {
			push = fmt.Sprintf("Push %d %s", a.git.ahead, "commit"+plural(a.git.ahead))
		}
		if glyphButton(c, icPush, "Push", push, a.git.ahead > 0, a.git.busy || noRemote).Clicked() {
			a.gitPush()
		}
	})
}

// aheadBehind is ↑ahead ↓behind, leaving out a zero.
func aheadBehind(ahead, behind int) string {
	var parts []string
	if ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", ahead))
	}
	if behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", behind))
	}
	return strings.Join(parts, " ")
}
