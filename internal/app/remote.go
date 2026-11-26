package app

import (
	"context"
	"fmt"
	"see/locales"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
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
	a.runSyncGit(doing, done, false, args...)
}

const gitFetchInterval = 2 * time.Minute

func (a *App) maybeFetchGit(now time.Time) {
	if !a.git.autoFetch || a.git.busy || a.git.loading || !a.hasRemote() ||
		!a.git.lastFetch.IsZero() && now.Sub(a.git.lastFetch) < gitFetchInterval {
		return
	}
	// Offline stands auto-fetch down whole: no process, no failure count,
	// no log. Nothing is scheduled to lift it — the fetch or push the
	// user runs when the network is back does, so recovery costs nothing
	// while the machine is away and nothing once it returns.
	if a.git.offline {
		return
	}
	// A fetch that cannot reach its remote backs off instead of spinning
	// every two minutes: the next try is a fetch interval later per
	// failure, up to eight.
	if a.git.fetchFailures > 0 {
		backoff := gitFetchInterval << min(a.git.fetchFailures-1, 3)
		if now.Sub(a.git.lastFetch) < backoff {
			return
		}
	}
	a.runSyncGit("Fetching…", "", true, "fetch", "--all", "--prune")
}

func (a *App) runSyncGit(doing, done string, background bool, args ...string) {
	if a.git.busy || a.git.root == "" {
		return
	}
	a.git.busy, a.git.syncing = true, doing
	a.git.autoFetching = background
	a.git.lastFetch = time.Now()
	ctx, root, gen, scopeSeq := a.wsCtx, a.git.root, a.gen, a.git.scopeSeq
	go func() {
		if background {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
		}
		_, err := git.Network(ctx, root, args...)
		a.post.Post(func() {
			if a.gen != gen || a.git.scopeSeq != scopeSeq {
				return
			}
			a.git.busy, a.git.syncing = false, ""
			a.git.autoFetching = false
			if err != nil {
				if background {
					// A background failure the network explains marks the app
					// offline once, quietly: the fetch was never the user's,
					// and the log line would repeat with every retry.
					if git.Reachable(err) {
						a.git.fetchFailures++
						a.logf("git auto-fetch: %v", err)
					} else {
						a.git.offline = true
					}
				} else {
					a.git.offline = !git.Reachable(err)
					a.note(locales.MsgGit()+gitErrLine(err), true)
				}
			} else {
				a.git.fetchFailures, a.git.offline = 0, false
				if background {
					// The status read that follows reports newly incoming
					// commits; a fetch of the user's does not, they are
					// already looking.
					a.git.fetchNotified = true
				} else {
					a.note(done, false)
					a.git.history.stale = true
					a.clearNotifyIncoming()
				}
				if args[0] == "pull" && !a.watching {
					a.refreshAll()
				}
			}
			a.refreshGit()
			a.refreshGitWorkingCopies()
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
	pull := locales.MsgPull() + tracks
	if a.git.behind > 0 {
		pull = locales.Catalog.Format(i18n.Locale(), "msgPullDIncomingCommits", map[string]any{"count": a.git.behind, "tracks": tracks})
	}
	if countButton(c, icPull, locales.MsgPull(), pull, a.git.behind, a.git.busy || a.git.upstream == "").Clicked() {
		a.gitPull()
	}
	push := locales.MsgPush() + tracks
	switch {
	case a.git.ahead > 0:
		push = locales.Catalog.Format(i18n.Locale(), "msgPushDCommits", map[string]any{"count": a.git.ahead, "tracks": tracks})
	case a.git.upstream == "":
		push = locales.Catalog.Format(i18n.Locale(), "msgPushSToTheRemoteS", map[string]any{"branch": a.git.branch, "remote": a.git.remotes[0]})
	}
	if countButton(c, icPush, locales.MsgPush(), push, a.git.ahead, a.git.busy).Clicked() {
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
