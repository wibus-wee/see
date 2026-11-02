package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"see/internal/git"
)

// The branch picker, as Xcode's: the branch under the project's name in
// the toolbar opens a list of the branches to switch to, local ones then
// remote ones, with a field that filters them and makes a new one.

type branchPicker struct {
	open     bool
	query    string
	sel      int
	list     ui.ListState
	branches []git.Branch
	loading  bool
	err      string
	seq      uint64
	at       ui.Rect // the button it hangs from
	anchor   ui.Rect // the toolbar's branch, as last drawn
}

// branchItem is a row of the picker: a section's heading, a branch, or
// the row that makes a branch of the query.
type branchItem struct {
	heading string
	branch  *git.Branch
	create  string
}

func (it branchItem) choosable() bool { return it.heading == "" }

// openBranchPicker shows the branches, read again each time it opens.
func (a *App) openBranchPicker(at ui.Rect) {
	if a.git.root == "" {
		a.note("This folder is not in a Git repository.", true)
		return
	}
	p := &a.git.picker
	p.open, p.query, p.sel, p.err, p.loading, p.at = true, "", 0, "", true, at
	p.list.Selected = &p.sel
	p.seq++
	seq, gen, ctx, root := p.seq, a.gen, a.wsCtx, a.git.root
	go func() {
		bs, err := git.Branches(ctx, root)
		a.post.Post(func() {
			if a.gen != gen || p.seq != seq {
				return
			}
			p.loading, p.branches = false, bs
			if err != nil {
				p.err = err.Error()
			}
			p.sel = firstChoosable(a.branchItems())
		})
	}()
}

// branchItems are the picker's rows for its query.
func (a *App) branchItems() []branchItem {
	p := &a.git.picker
	q := strings.ToLower(strings.TrimSpace(p.query))
	var items []branchItem
	exact := false
	section := func(heading string, remote bool) {
		first := true
		for i := range p.branches {
			b := &p.branches[i]
			if b.Remote != remote || q != "" && !strings.Contains(strings.ToLower(b.Name), q) {
				continue
			}
			if strings.EqualFold(b.Name, strings.TrimSpace(p.query)) {
				exact = true
			}
			if first {
				items = append(items, branchItem{heading: heading})
				first = false
			}
			items = append(items, branchItem{branch: b})
		}
	}
	section("Branches", false)
	section("Remote Branches", true)
	// Last, so that Return takes the first branch that matches.
	if name := strings.TrimSpace(p.query); name != "" && !exact {
		items = append(items, branchItem{create: name})
	}
	return items
}

func firstChoosable(items []branchItem) int {
	for i, it := range items {
		if it.choosable() {
			return i
		}
	}
	return 0
}

// moveBranchSel moves the selection by dir, over the headings.
func (a *App) moveBranchSel(items []branchItem, dir int) {
	p := &a.git.picker
	for i := p.sel + dir; i >= 0 && i < len(items); i += dir {
		if items[i].choosable() {
			p.sel = i
			p.list.ScrollTo(i, ui.Center)
			return
		}
	}
}

func (a *App) chooseBranch(it branchItem) {
	p := &a.git.picker
	switch {
	case it.create != "":
		p.open = false
		a.runBranchJob("Created and switched to "+it.create+".", func() error {
			return git.CreateBranch(a.wsCtx, a.git.root, it.create)
		})
	case it.branch != nil && it.branch.Current:
		p.open = false
	case it.branch != nil:
		p.open = false
		b, all := *it.branch, p.branches
		a.runBranchJob("Switched to "+b.LocalName()+".", func() error {
			return git.Switch(a.wsCtx, a.git.root, b, all)
		})
	}
}

// runBranchJob switches the working tree off the UI thread; the files it
// changes come back through the watcher, and the tree is read again for
// a folder that is not watched.
func (a *App) runBranchJob(done string, job func() error) {
	if a.git.busy {
		return
	}
	a.git.busy = true
	gen := a.gen
	go func() {
		err := job()
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			a.git.busy = false
			if err != nil {
				a.note("Git: "+gitErrLine(err), true)
			} else {
				a.note(done, false)
				if !a.watching {
					a.refreshAll()
				}
			}
			a.refreshGit()
		})
	}()
}

// gitErrLine is the part of a git error worth a line: its fatal or error
// message, else its first line.
func gitErrLine(err error) string {
	s := err.Error()
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "error: "); i >= 0 {
			return line[i+len("error: "):]
		}
		if i := strings.Index(line, "fatal: "); i >= 0 {
			return line[i+len("fatal: "):]
		}
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// branchButton is the branch under the project's name in the toolbar.
func (a *App) branchButton(c *ui.Context) {
	if a.git.branch == "" {
		return
	}
	a.git.picker.anchor = a.branchHandle.Bounds(c)
	b := ui.ButtonBase(c).Label("Switch Branch").Tooltip("Switch Branch: "+a.git.branch).
		Padding(0, 4, 0, 2).Radius(5).Shrink(1).KeepFocus().Bind(&a.branchHandle)
	if b.Hovered() || b.Pressed() || a.git.picker.open {
		b.Background(colors(c).Pill)
	}
	b.OnClick(func() {
		if a.git.picker.open {
			a.git.picker.open = false
			return
		}
		a.openBranchPicker(a.git.picker.anchor)
	})
	b.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			symbolIcon(c, icGit, 11, colors(c).Muted).Shrink(0)
			ui.Text(c, a.git.branch).FontSize(11).TextColor(colors(c).Muted).SingleLine().Shrink(1)
			symbolIcon(c, icChevron, 8, colors(c).Dim).Shrink(0)
		})
	})
}

// branchPickerPanel is the picker's popover, under its button.
func (a *App) branchPickerPanel(c *ui.Context) {
	p := &a.git.picker
	items := a.branchItems()
	p.sel = min(max(p.sel, 0), max(len(items)-1, 0))
	ui.Overlay(c, func() {
		backdrop := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0)
		backdrop.OnClick(func() { p.open = false })
		panel := ui.Column(c).Absolute().Left(max(p.at.X-8, 8)).Top(p.at.Y+p.at.H+6).Width(360).MaxHeight(440).
			Radius(12).Padding(6).Gap(4).Material(glass.Glass{}).Border(1, colors(c).PopoverEdge).
			Shadow(0, 12, 36, 0, ui.RGBA(0, 0, 0, 0.35))
		panel.OnClick(func() {})
		panel.Children(func() {
			ui.Row(c).Padding(4, 6).Gap(6).AlignItems(ui.Center).Children(func() {
				symbolIcon(c, icSearch, 14, colors(c).Muted)
				in := ui.TextInput(c, &p.query).Placeholder("Filter, or name a new branch").Label("Filter branches").
					FontSize(13).Grow(1).AutoFocus().Background(ui.Transparent).BorderWidth(0)
				switch {
				case in.Changed():
					p.sel = firstChoosable(a.branchItems())
				case in.Submitted():
					if p.sel < len(items) {
						a.chooseBranch(items[p.sel])
					}
				case in.Shortcut(0, ui.KeyEscape):
					p.open = false
				case in.Shortcut(0, ui.KeyDown):
					a.moveBranchSel(items, 1)
				case in.Shortcut(0, ui.KeyUp):
					a.moveBranchSel(items, -1)
				}
			})
			ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator)
			switch {
			case p.loading:
				ui.Row(c).Padding(8).Gap(8).Children(func() {
					ui.Spinner(c)
					ui.Text(c, "Reading branches…").FontSize(12).TextColor(colors(c).Muted)
				})
			case p.err != "":
				ui.Text(c, p.err).FontSize(12).TextColor(colors(c).Err).Padding(8)
			case len(items) == 0:
				ui.Text(c, "No Branches").FontSize(12).TextColor(colors(c).Muted).Padding(8)
			default:
				ui.List(c, &p.list, len(items), func(i int) { a.branchRow(c, items, i) }).Grow(1).FillWidth().MaxHeight(380).Label("Branches")
			}
		})
	})
}

func (a *App) branchRow(c *ui.Context, items []branchItem, i int) {
	p := &a.git.picker
	it := items[i]
	if it.heading != "" {
		ui.Text(c.Key(i), it.heading).FontSize(11).Bold().TextColor(colors(c).Muted).Padding(8, 8, 3, 8)
		return
	}
	selected := i == p.sel
	text, sub := colors(c).Text, colors(c).Muted
	row := ui.Row(c.Key(i)).FillWidth().Padding(5, 8).Gap(8).Radius(6).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if selected {
		row.Background(c.Theme().Accent)
		text, sub = ui.RGB(0xff, 0xff, 0xff), ui.RGBA(0xff, 0xff, 0xff, 0.75)
	}
	if row.Hovered() && !selected {
		row.Background(colors(c).Pill)
	}
	row.OnClick(func() { a.chooseBranch(it) })
	row.Children(func() {
		if it.create != "" {
			symbolIcon(c, icPlus, 13, text)
			ui.Text(c, "Create Branch “"+it.create+"”").FontSize(13).TextColor(text).SingleLine().Shrink(1)
			return
		}
		b := it.branch
		check := ui.Box(c).Size(13, 13).Shrink(0)
		if b.Current {
			check.Children(func() { symbolIcon(c, icCheck, 13, text) })
		}
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Text(c, b.Name).FontSize(13).TextColor(text).SingleLine()
			detail := b.Subject
			if b.Track != "" {
				detail = b.Track + " · " + detail
			}
			ui.Text(c, detail).FontSize(11).TextColor(sub).SingleLine()
		})
		if !b.Date.IsZero() {
			ui.Text(c, relativeTime(b.Date, time.Now())).FontSize(11).TextColor(sub).SingleLine().Shrink(0)
		}
	})
}

// relativeTime tells how long ago t was, as Xcode's source control
// navigator: "5 min ago", "Yesterday", or the date.
func relativeTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "Just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 24*time.Hour && t.Day() == now.Day():
		return fmt.Sprintf("%d hr ago", int(d/time.Hour))
	case d < 48*time.Hour && t.Day() == now.AddDate(0, 0, -1).Day():
		return "Yesterday"
	case d < 7*24*time.Hour:
		return t.Format("Monday")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}
