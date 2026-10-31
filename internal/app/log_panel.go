package app

import (
	"fmt"
	"log"
	"log/slog"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/langserver"
	"see/internal/theme"
)

// serverLog is a logger for a language server: it keeps the line in the
// app's output log and draws the window again.
func (a *App) serverLog(source string) func(string, ...any) {
	return func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		a.logs.Add(slog.LevelInfo, source, msg)
		log.Printf("%s: %s", source, msg)
		a.post(func() {})
	}
}

// serverStateLog notes a language server's state in the output log.
func (a *App) serverStateLog(source string, state langserver.State, err error) {
	msg := ""
	level := slog.LevelInfo
	switch state {
	case langserver.Starting:
		msg = "Starting"
	case langserver.Ready:
		msg = "Ready"
	case langserver.Failed:
		level = slog.LevelError
		msg = fmt.Sprintf("Failed: %v", err)
	}
	if msg != "" {
		a.logs.Add(level, source, msg)
	}
}

// logView draws the output panel.
func (a *App) logView(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(6, 12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Output").Bold().Grow(1)
			if ui.Button(c, "Clear").Label("Clear Output").Clicked() {
				a.logs.Clear()
			}
		})
		entries := a.logs.Snapshot()
		if len(entries) == 0 {
			ui.Text(c, "No output yet.").TextColor(theme.Of(c).Muted).Padding(12)
			return
		}
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(2, 12, 12, 12).Gap(3).Children(func() {
				for i, e := range entries {
					color := theme.Of(c).Muted
					if e.Level >= slog.LevelError {
						color = theme.Of(c).Err
					}
					ui.Text(c.Key(i), e.At.Format("15:04:05")+"  "+strings.ToUpper(e.Level.String())+"  "+e.Source+"  "+e.Message).FontSize(12).TextColor(color)
				}
			})
		})
	})
}
