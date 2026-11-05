package app

import (
	"fmt"
	"see/locales"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/git"
)

// Fetch, Pull and Push, as Xcode's Source Control menu, and how far the
// branch is from the one it tracks, as Peek counts incoming commits.

var (
	icPull = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 4v14M6 12l6 6 6-6"/></svg>`))
	icPush = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20V6M6 12l6-6 6 6"/></svg>`))
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
				a.note(locales.MsgGit()+gitErrLine(err), true)
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
		a.note(locales.MsgTheBranchTracksNoRemoteBranchToPull(), true)
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
		a.note(locales.MsgTheRepositoryHasNoRemoteToPushTo(), true)
	default:
		remote := a.git.remotes[0]
		a.syncGit("Pushing…", "Pushed to "+remote+"/"+a.git.branch+".", "push", "--set-upstream", remote, "HEAD")
	}
}

// syncButtons are Pull and Push, in the Git navigator's title line;
// they take the accent, and their count, when there are commits to bring
// or send. Fetch is the refresh button's, as Xcode's Fetch and Refresh
// Status.
func (a *App) syncButtons(c *ui.Context) {
	if !a.hasRemote() {
		return
	}
	tracks := ""
	if a.git.upstream != "" {
		tracks = " (" + a.git.upstream + ")"
	}
	pull := "Pull" + tracks
	if a.git.behind > 0 {
		pull = fmt.Sprintf("Pull %d incoming commit%s%s", a.git.behind, plural(a.git.behind), tracks)
	}
	if countButton(c, icPull, "Pull", pull, a.git.behind, a.git.busy || a.git.upstream == "").Clicked() {
		a.gitPull()
	}
	push := "Push" + tracks
	switch {
	case a.git.ahead > 0:
		push = fmt.Sprintf("Push %d commit%s%s", a.git.ahead, plural(a.git.ahead), tracks)
	case a.git.upstream == "":
		push = "Push " + a.git.branch + " to " + a.git.remotes[0]
	}
	if countButton(c, icPush, "Push", push, a.git.ahead, a.git.busy).Clicked() {
		a.gitPush()
	}
}

func (a *App) hasRemote() bool { return len(a.git.remotes) > 0 || a.git.upstream != "" }

// fetchAndRefresh reads the repository again, after fetching from its
// remotes when it has any.
func (a *App) fetchAndRefresh() {
	a.git.history.stale = true
	if a.hasRemote() {
		a.gitFetch()
		return
	}
	a.refreshGit()
}

// countButton is a glyph button with a count beside its glyph, in the
// accent, while there is one.
func countButton(c *ui.Context, icon *ui.SVG, label, tip string, n int, disabled bool) ui.Element {
	if n == 0 {
		return glyphButton(c, icon, label, tip, false, disabled)
	}
	b := ui.ButtonBase(c.Key(label)).Label(label).Tooltip(tip).Padding(3, 4, 3, 3).Radius(4).Shrink(0).KeepFocus().Disabled(disabled)
	if !disabled && (b.Hovered() || b.Pressed()) {
		b.Background(colors(c).Pill)
	}
	accent := c.Theme().Accent
	b.Children(func() {
		ui.Row(c).Gap(1).AlignItems(ui.Center).Children(func() {
			symbolIcon(c, icon, 14, accent)
			ui.Text(c, fmt.Sprint(n)).FontSize(10).Bold().TextColor(accent)
		})
	})
	return b
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
