package main

import (
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

type logEntry struct {
	at              time.Time
	level           slog.Level
	source, message string
}
type logStore struct {
	mu      sync.Mutex
	entries []logEntry
}

func (s *logStore) add(level slog.Level, source, message string) {
	if len(message) > 8192 {
		message = message[:8192] + "…"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, logEntry{time.Now(), level, source, message})
	if len(s.entries) > 1000 {
		s.entries = append([]logEntry(nil), s.entries[len(s.entries)-1000:]...)
	}
}
func (s *logStore) snapshot() []logEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]logEntry(nil), s.entries...)
}
func (s *logStore) clear() { s.mu.Lock(); defer s.mu.Unlock(); s.entries = nil }

func (a *app) serverLog(source string) func(string, ...any) {
	return func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		a.logs.add(slog.LevelInfo, source, msg)
		log.Printf("%s: %s", source, msg)
		a.post(func() {})
	}
}
func (a *app) serverStateLog(source string, state lspState, err error) {
	msg := ""
	level := slog.LevelInfo
	switch state {
	case lspStarting:
		msg = "Starting"
	case lspReady:
		msg = "Ready"
	case lspFailed:
		level = slog.LevelError
		msg = fmt.Sprintf("Failed: %v", err)
	}
	if msg != "" {
		a.logs.add(level, source, msg)
	}
}
func (a *app) logView(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).FillWidth().Padding(6, 12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Output").Bold().Grow(1)
			if ui.Button(c, "Clear").Label("Clear Output").Clicked() {
				a.logs.clear()
			}
		})
		entries := a.logs.snapshot()
		if len(entries) == 0 {
			ui.Text(c, "No output yet.").TextColor(colMuted).Padding(12)
			return
		}
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(2, 12, 12, 12).Gap(3).Children(func() {
				for i, e := range entries {
					color := colMuted
					if e.level >= slog.LevelError {
						color = colError
					}
					ui.Text(c.Key(i), e.at.Format("15:04:05")+"  "+strings.ToUpper(e.level.String())+"  "+e.source+"  "+e.message).FontSize(12).TextColor(color)
				}
			})
		})
	})
}
