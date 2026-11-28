package config

import (
	"path/filepath"
	"testing"
)

func TestEditorFontSizeRoundTrip(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "config.json"))
	for _, tc := range []struct{ stored, want float64 }{
		{0, 13}, {9, 9}, {16, 16}, {28, 28},
	} {
		c.FontSize = tc.stored
		if err := c.Save(); err != nil {
			t.Fatal(err)
		}
		loaded := Load(c.Path())
		if loaded.FontSize != tc.stored || loaded.EditorFontSize() != tc.want {
			t.Fatalf("stored %g: raw=%g effective=%g, want %g", tc.stored, loaded.FontSize, loaded.EditorFontSize(), tc.want)
		}
	}
}

// Out-of-range font sizes are invalid configuration: the load reports a
// validation failure and falls back to the default. The invalid value is
// representable in memory, so write-back converges the file to that
// in-memory truth — the file heals on the first save instead of warning
// forever.
func TestEditorFontSizeOutOfRangeDegrades(t *testing.T) {
	for _, stored := range []float64{8, 29} {
		p := filepath.Join(t.TempDir(), "config.json")
		c := Load(p)
		c.FontSize = stored
		if err := c.Save(); err != nil {
			t.Fatal(err)
		}
		loaded := Load(p)
		if loaded.FontSize != 0 || loaded.EditorFontSize() != 13 {
			t.Fatalf("stored %g: raw=%g effective=%g, want defaults", stored, loaded.FontSize, loaded.EditorFontSize())
		}
		if len(loaded.Issues()) != 1 || loaded.Issues()[0].Kind != IssueValidationFailed || loaded.Issues()[0].Key != "editorFontSize" {
			t.Fatalf("stored %g: issues %+v, want one validation failure", stored, loaded.Issues())
		}
		// Write-back converges the file to the in-memory truth: the
		// invalid value is replaced by the degraded default — the key
		// omitted — and the next load reports nothing.
		if err := loaded.Save(); err != nil {
			t.Fatal(err)
		}
		again := Load(p)
		if len(again.Issues()) != 0 {
			t.Fatalf("stored %g: the file did not heal on write-back: %+v", stored, again.Issues())
		}
	}
}
