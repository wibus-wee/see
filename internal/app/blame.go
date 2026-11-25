package app

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

func (a *App) syncBlame(d *doc) {
	if d.buf == nil || d.lossy || d.plain {
		return
	}
	key, version := a.gitBaseKey(d), d.buf.Version()
	if key == d.blameKey && version == d.blameVersion {
		return
	}
	d.blameSeq++
	if d.blameTimer != nil {
		d.blameTimer.Stop()
	}
	if d.blameCancel != nil {
		d.blameCancel()
	}
	d.blameKey, d.blameVersion, d.blameReady, d.blame = key, version, false, nil
	rel, ok := a.gitRelPath(d.path)
	if key == "" || !ok || a.wsCtx == nil {
		return
	}
	seq, gen, root := d.blameSeq, a.gen, a.git.root
	ctx, cancel := context.WithCancel(a.wsCtx)
	d.blameCancel = cancel
	start := func() {
		if ctx.Err() != nil || a.gen != gen || a.docs[d.path] != d || d.blameSeq != seq || d.buf.Version() != version {
			return
		}
		contents := []byte(d.buf.String())
		go func() {
			defer cancel()
			_, err := git.Command(ctx, root, "ls-files", "--error-unmatch", "--", rel)
			var lines []git.BlameLine
			if err == nil {
				lines, err = git.Blame(ctx, root, rel, contents)
			}
			a.post.Post(func() {
				if a.gen != gen || a.docs[d.path] != d || d.blameSeq != seq || d.buf.Version() != version || a.gitBaseKey(d) != key {
					return
				}
				d.blameReady = true
				if err == nil {
					d.blame = lines
				}
			})
		}()
	}
	d.blameTimer = time.AfterFunc(250*time.Millisecond, func() { a.post.Post(start) })
}

func (a *App) lineBlame(d *doc, st *editor.State) (git.BlameLine, bool) {
	if d == nil || st == nil || d.buf == nil || !d.blameReady || d.blameVersion != d.buf.Version() || d.blameKey != a.gitBaseKey(d) {
		return git.BlameLine{}, false
	}
	line := st.CursorPos().Line
	if line < 0 || line >= len(d.blame) {
		return git.BlameLine{}, false
	}
	return d.blame[line], true
}

func blameInlineText(b git.BlameLine) string {
	if b.Uncommitted {
		return logText("blameUncommitted")
	}
	return fmt.Sprintf(logText("blameInlineFormat"), b.Author, blameDate(b.Date), b.Subject)
}

func blameDate(t time.Time) string {
	return relativeTime(t, time.Now())
}

func parameterHintModifier(goos string) ui.Modifiers {
	if goos == "darwin" {
		return ui.Alt
	}
	return ui.Ctrl | ui.Alt
}

func (a *App) editorInlays(d *doc, st *editor.State) []editor.Inlay {
	var inlays []editor.Inlay
	if !a.cfg.HideTypeHints {
		inlays = append(inlays, d.languageInlays...)
	}
	modifier := parameterHintModifier(runtime.GOOS)
	if a.modifiers&modifier == modifier && a.cfg.IsTrusted(a.root) {
		inlays = append(inlays, d.parameterInlays...)
	}
	cursorDiagnostic := false
	for _, diagnostic := range a.inlineDiagnostics(d) {
		line := diagnostic.line
		offset := d.buf.Offset(editor.Pos{Line: line, Col: len(d.buf.Line(line))})
		inlays = append(inlays, editor.Inlay{Offset: offset, Text: "    " + diagnostic.message, Trailing: true, Ellipsis: "…"})
		cursorDiagnostic = cursorDiagnostic || line == st.CursorPos().Line
	}
	if b, ok := a.lineBlame(d, st); ok && !a.cfg.HideInlineBlame && !cursorDiagnostic {
		line := st.CursorPos().Line
		offset := d.buf.Offset(editor.Pos{Line: line, Col: len(d.buf.Line(line))})
		inlays = append(inlays, editor.Inlay{Offset: offset, Text: "    " + blameInlineText(b), Trailing: true, Ellipsis: "…"})
	}
	return inlays
}

func (a *App) blameStatus(c *ui.Context, d *doc, st *editor.State) {
	b, ok := a.lineBlame(d, st)
	if !ok {
		return
	}
	if b.Uncommitted {
		ui.Text(c, logText("blameUncommitted")).FontSize(11).TextColor(colors(c).Muted).SingleLine()
		return
	}
	label := strings.Join([]string{b.Short, b.Author, blameDate(b.Date)}, " · ")
	button := ui.ButtonBase(c).Label(label).Tooltip(b.Subject+"\n"+b.Date.Local().Format("2006-01-02 15:04:05 MST")+"\n"+commandLabel("git.revealLineCommit")).
		Padding(2, 5).Radius(4).MaxWidth(480).Shrink(1).Cursor(ui.CursorPointer)
	if button.Hovered() {
		button.Background(colors(c).Pill)
	}
	button.OnClick(func() { a.revealLogCommit(b.Hash) }).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(colors(c).Muted).SingleLine()
	})
}

func (a *App) canRevealLineCommit() bool {
	if a.focus == nil {
		return false
	}
	b, ok := a.lineBlame(a.docs[a.focus.active], a.activeEditor())
	return ok && !b.Uncommitted
}

func (a *App) revealLineCommit() {
	if a.canRevealLineCommit() {
		b, _ := a.lineBlame(a.docs[a.focus.active], a.activeEditor())
		a.revealLogCommit(b.Hash)
	}
}
