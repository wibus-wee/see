package config

import (
	"path/filepath"
	"testing"
)

func TestEditorFontSizeRoundTrip(t *testing.T) {
	c := Load(filepath.Join(t.TempDir(), "config.json"))
	for _, tc := range []struct{ stored, want float64 }{
		{0, 13}, {8, 13}, {9, 9}, {16, 16}, {28, 28}, {29, 13},
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
