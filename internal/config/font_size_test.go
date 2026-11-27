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
// validation failure, falls back to the default, and preserves the file's
// text for write-back rather than silently rewriting or accepting it.
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
		// Saving again must not lose the original value: the file keeps
		// saying what it said until the user fixes it.
		if err := loaded.Save(); err != nil {
			t.Fatal(err)
		}
		again := Load(p)
		if len(again.Issues()) != 1 || again.Issues()[0].Key != "editorFontSize" {
			t.Fatalf("stored %g: the invalid value was lost or repaired silently: %+v", stored, again.Issues())
		}
	}
}
