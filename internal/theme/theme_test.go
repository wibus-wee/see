package theme

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestForChoosesTheAppearance(t *testing.T) {
	if For(true) != &Dark || For(false) != &Light {
		t.Fatal("For chose the wrong palette")
	}
	if Dark.Editor == Light.Editor {
		t.Fatal("the appearances share a color")
	}
}

func TestNewKeepsTheDesktopsAccent(t *testing.T) {
	accent := ui.RGB(0x12, 0x34, 0x56)
	for _, dark := range []bool{false, true} {
		base := &ui.Theme{Dark: dark, Accent: accent}
		ut, ed := New(base, 2)
		p := For(dark)
		if ut.Accent != accent {
			t.Fatalf("dark=%v: accent = %v, want %v", dark, ut.Accent, accent)
		}
		if ut.Background != p.Editor || ut.Text != p.Text || ut.Border != p.Separator {
			t.Fatalf("dark=%v: colors are not the palette's", dark)
		}
		if ut.FontSize != 26 {
			t.Fatalf("dark=%v: font size = %v, want 13 at double the text size", dark, ut.FontSize)
		}
		if ed == nil || ed.Background != p.Editor {
			t.Fatalf("dark=%v: the editors' theme does not follow", dark)
		}
	}
}
