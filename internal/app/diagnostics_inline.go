package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

type inlineDiagnostic struct {
	line     int
	message  string
	severity editor.Severity
	count    int
}

func (a *App) inlineDiagnostics(d *doc) []inlineDiagnostic {
	if a.cfg.HideInlineDiagnostics || d.buf == nil || d.diagnosticVersion != d.buf.Version() {
		return nil
	}
	byLine := make(map[int]inlineDiagnostic)
	for _, diagnostic := range d.diagnostics {
		if diagnostic.Severity > editor.SeverityInfo || diagnostic.Start < 0 || diagnostic.Start > d.buf.Len() {
			continue
		}
		line := d.buf.Pos(diagnostic.Start).Line
		entry := byLine[line]
		if entry.count == 0 || diagnostic.Severity < entry.severity {
			message, _, _ := strings.Cut(diagnostic.Message, "\n")
			entry.message = strings.Join(strings.Fields(message), " ")
			entry.severity = diagnostic.Severity
		}
		entry.line, entry.count = line, entry.count+1
		byLine[line] = entry
	}
	lines := make([]inlineDiagnostic, 0, len(byLine))
	for _, entry := range byLine {
		if entry.count > 1 {
			entry.message += fmt.Sprintf(" (+%d)", entry.count-1)
		}
		lines = append(lines, entry)
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].line < lines[j].line })
	return lines
}

func (a *App) diagnosticLineBackgrounds(d *doc, st *editor.State) []editor.LineBackground {
	backgrounds := append([]editor.LineBackground(nil), st.LineBackgrounds...)
	for _, diagnostic := range a.inlineDiagnostics(d) {
		color := st.Theme.Error
		switch diagnostic.severity {
		case editor.SeverityWarning:
			color = st.Theme.Warning
		case editor.SeverityInfo:
			color = st.Theme.Info
		}
		backgrounds = append(backgrounds, editor.LineBackground{First: diagnostic.line, Last: diagnostic.line, Color: color.Alpha(0.10)})
	}
	return backgrounds
}
