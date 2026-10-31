package logs

import (
	"log/slog"
	"strings"
	"testing"
)

func TestAddKeepsTheLatestEntries(t *testing.T) {
	var s Store
	for i := 0; i < maxEntries+5; i++ {
		s.Add(slog.LevelInfo, "gopls", "line")
	}
	entries := s.Snapshot()
	if len(entries) != maxEntries {
		t.Fatalf("len = %d, want %d", len(entries), maxEntries)
	}
}

func TestAddShortensALongLine(t *testing.T) {
	var s Store
	s.Add(slog.LevelError, "tsrs", strings.Repeat("x", maxMessage+10))
	entries := s.Snapshot()
	if len(entries) != 1 {
		t.Fatalf("len = %d", len(entries))
	}
	if got := len([]rune(entries[0].Message)); got != maxMessage+1 {
		t.Fatalf("message runes = %d, want %d and an ellipsis", got, maxMessage+1)
	}
	if entries[0].Level != slog.LevelError || entries[0].Source != "tsrs" {
		t.Fatalf("entry = %+v", entries[0])
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	var s Store
	s.Add(slog.LevelInfo, "app", "one")
	entries := s.Snapshot()
	entries[0].Message = "changed"
	if got := s.Snapshot()[0].Message; got != "one" {
		t.Fatalf("the store changed under the frame: %q", got)
	}
}

func TestClear(t *testing.T) {
	var s Store
	s.Add(slog.LevelInfo, "app", "one")
	s.Clear()
	if len(s.Snapshot()) != 0 {
		t.Fatal("clear kept entries")
	}
}
