package app

import (
	"testing"
)

// A file's icon is its name's, else its longest extension's, without
// regard to case, else the theme's file.
func TestFileIconOf(t *testing.T) {
	th := &fileIconTheme
	for name, want := range map[string]string{
		"package.json": th.names["package.json"],
		"Dockerfile":   th.names["dockerfile"],
		"main.TS":      th.extensions["ts"],
		"README.md":    th.extensions["md"],
		"notes":        th.file,
		"archive.xyz":  th.file,
	} {
		if want == "" {
			t.Fatalf("the theme has nothing for %q", name)
		}
		if got := th.fileIconOf(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
	precedence := iconTheme{file: "default", names: map[string]string{"special.d.ts": "named"}, extensions: map[string]string{"ts": "short", "d.ts": "compound"}}
	for name, want := range map[string]string{"special.d.ts": "named", "other.D.TS": "compound", "other.ts": "short", "other.unknown": "default"} {
		if got := precedence.fileIconOf(name); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// Every icon the theme names parses, for either appearance.
func TestFileIconsParse(t *testing.T) {
	th := &fileIconTheme
	icons := map[string]bool{th.file: true, th.folder: true, th.folderOpen: true, th.symlink: true}
	for _, m := range []map[string]string{th.names, th.extensions, th.folders} {
		for _, icon := range m {
			icons[icon] = true
		}
	}
	for icon := range icons {
		for _, dark := range []bool{false, true} {
			key := icon
			if !dark {
				key += "_light"
			}
			if _, err := fileIconFiles.ReadFile("icons/files/" + key + ".svg"); err != nil {
				t.Errorf("no %s: %v", key, err)
				continue
			}
			fileIconSVG(icon, dark)
		}
	}
}
