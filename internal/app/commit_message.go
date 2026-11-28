package app

import (
	"slices"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"see/internal/git"
	"see/locales"
)

const (
	commitMessageSubjectSize = 18
	commitMessageBodySize    = 13
)

// commitDetailsBlock is what every commit detail shows: the message,
// who made it and when, and the same meta rows. The files below it are
// the caller's, as each place shows them.
func (a *App) commitDetailsBlock(c *ui.Context, v *commitView, openInTab bool) {
	a.commitMessageHeader(c, v, openInTab)
	a.commitMeta(c, v)
	if v.message.Body != "" {
		ui.Text(c, v.message.Body).FontSize(commitMessageBodySize).Selectable().FillWidth()
	}
}

// commitDetailContent is what both details draw below their scroll:
// the message and meta, then the files, or what is loading or failed.
func (a *App) commitDetailContent(c *ui.Context, g *group, v *commitView, openInTab bool) {
	a.commitDetailsBlock(c, v, openInTab)
	ui.Box(c).Height(1).FillWidth().Background(colors(c).Separator)
	switch {
	case v.loading:
		ui.Row(c).Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.MsgReadingTheCommit()).FontSize(12).TextColor(colors(c).Muted)
		})
	case v.err != "":
		ui.Text(c, v.err).FontSize(12).TextColor(colors(c).Err)
	default:
		if !v.filesLoaded {
			if !v.filesLoading {
				a.loadCommitFilesView(v)
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.MsgReadingTheCommit()).FontSize(12).TextColor(colors(c).Muted)
			})
			return
		}
		a.commitFilesBlock(c, g, v)
	}
}

// commitMessageHeader draws a commit's subject and its author and
// co-authors.
func (a *App) commitMessageHeader(c *ui.Context, v *commitView, openInTab bool) {
	cm, msg := v.commit, v.message
	// The log line's subject is the one lists show, and a stash replaces
	// it with the stash's message; the raw message only fills it in when
	// the log line has none.
	subject := cm.Subject
	if subject == "" {
		subject = msg.Subject
	}
	// Not read yet: the log line already tells the co-authors.
	coAuthors := cm.CoAuthors
	if !v.loading && v.err == "" {
		coAuthors = msg.CoAuthors
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, subject).FontSize(commitMessageSubjectSize).Bold().Selectable().Grow(1).Shrink(1)
		if openInTab {
			a.commitOpenInTabButton(c, cm)
		}
		a.commitHostButton(c, cm)
	})
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		a.personLine(c, git.Person{Name: cm.Author, Email: cm.Email}, "", commitWhen(cm, v.details))
		for _, p := range coAuthors {
			a.personLine(c.Key(p.Email), p, locales.MsgCoAuthor(), p.Email)
		}
	})
}

func commitWhen(cm git.Commit, d git.CommitDetails) string {
	when := fullDate(cm.Date)
	if d.Committer != "" && d.Committer != cm.Author {
		when += " · " + locales.Catalog.Format(i18n.Locale(), "gitCommittedBy", map[string]any{"name": d.Committer})
	}
	return when
}

// commitMeta is the short hash, signature and refs under every commit
// message.
func (a *App) commitMeta(c *ui.Context, v *commitView) {
	cm, d := v.commit, v.details
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Text(c, cm.Short).FontSize(12).TextColor(colors(c).Muted).Selectable().Tooltip(cm.Hash)
		if glyphButton(c, icCopy, locales.MsgCopyCommitHash(), locales.MsgCopyCommitHash(), false, false).Clicked() {
			c.Services().WriteClipboard(cm.Hash)
			a.flash(locales.MsgCopied() + cm.Short + ".")
		}
		a.commitSignatureBadge(c, d.Signature)
		a.commitRefs(c, cm)
	})
}

func (a *App) commitOpenInTabButton(c *ui.Context, cm git.Commit) {
	if glyphButton(c, icOpenInTab, locales.MsgOpenInTab(), locales.MsgOpenInTab(), false, false).Clicked() {
		a.openCommit(cm)
	}
}

func (a *App) commitHostButton(c *ui.Context, cm git.Commit) {
	link := a.commitLink(cm.Hash)
	if link == "" {
		return
	}
	label := locales.Catalog.Format(i18n.Locale(), "editorContextOpenOnHost", map[string]any{"host": hostName(a.git.web)})
	if glyphButton(c, icLink, label, label, false, false).Clicked() {
		c.Services().OpenURL(link)
	}
}

// visibleCommitRefs are the refs a commit shows: the checked-out
// branch when HEAD points at it, and one tag, or the first ref when
// neither. active is the branch to mark, "" for none.
func visibleCommitRefs(cm git.Commit, current string) (shown []string, active string) {
	refs := make([]string, 0, len(cm.Refs))
	for _, ref := range cm.Refs {
		if ref != "HEAD" {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil, ""
	}
	if slices.Contains(cm.Refs, "HEAD") && current != "" && slices.Contains(refs, current) {
		shown = append(shown, current)
		active = current
	}
	for _, tag := range cm.Tags {
		if !slices.Contains(shown, tag) {
			shown = append(shown, tag)
			break
		}
	}
	if len(shown) == 0 {
		shown = append(shown, refs[0])
	}
	return shown, active
}

func (a *App) commitRefs(c *ui.Context, cm git.Commit) {
	shown, active := visibleCommitRefs(cm, a.git.branch)
	for _, ref := range shown {
		refBadge(c, ref, ref == active)
	}
}

type commitSubjectStyle int

const (
	commitSubjectRow commitSubjectStyle = iota
	commitSubjectPreview
	commitSubjectCaption
)

// commitSubject is a commit's subject where a list or a preview shows
// it on one line, with the sizes and truncation every such place shares.
func commitSubject(c *ui.Context, subject string, style commitSubjectStyle) ui.Element {
	text := ui.Text(c, subject)
	switch style {
	case commitSubjectRow:
		text.FontSize(12).FontWeight(600)
	case commitSubjectPreview:
		text.FontSize(13)
	case commitSubjectCaption:
		text.FontSize(11)
	}
	if style == commitSubjectPreview {
		return text
	}
	return text.SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
}

// commitSignatureBadge is the key a commit was signed with; an
// unsigned commit shows none. It does not verify the signature.
func (a *App) commitSignatureBadge(c *ui.Context, sig git.CommitSignature) {
	if !sig.Present() {
		return
	}
	label := locales.MsgCommitSignatureKeyID(sig.Key)
	badge := ui.Row(c).Height(18).Padding(0, 6).Radius(4).Gap(4).AlignItems(ui.Center).Shrink(0).
		Background(colors(c).Pill).Label(label)
	badge.Children(func() {
		symbolIcon(c, icKey, 11, colors(c).Muted)
		ui.Text(c, label).FontSize(11).TextColor(colors(c).Muted).SingleLine().Shrink(0)
	})
}
