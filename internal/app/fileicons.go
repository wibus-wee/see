package app

import (
	"embed"
	"strings"

	"github.com/egoist/mygo/ui"
)

// A file's icon is as VS Code shows it with an icon theme, as kero's:
// Pierre's (icons/files, brought in with scripts/fileicons), in their own
// colors, for the light appearance or the dark one. A file's is that of
// its name, else of its longest extension, else the theme's file; a
// folder's is the theme's folder, open or not (folderGlyph).

//go:embed icons/files/*.svg
var fileIconFiles embed.FS

// iconTheme is an icon theme's icons, by the names, extensions and
// folders they are for, lowercase (fileicons_gen.go).
type iconTheme struct {
	file, folder, folderOpen, symlink string
	names, extensions, folders        map[string]string
}

// fileIconOf is the icon of a file of the theme.
func (t *iconTheme) fileIconOf(name string) string {
	name = strings.ToLower(name)
	if icon, ok := t.names[name]; ok {
		return icon
	}
	// The longest extension first: "d.ts" before "ts".
	for i := strings.IndexByte(name, '.'); i >= 0; {
		if icon, ok := t.extensions[name[i+1:]]; ok {
			return icon
		}
		j := strings.IndexByte(name[i+1:], '.')
		if j < 0 {
			break
		}
		i += 1 + j
	}
	return t.file
}

// fileIcons are the theme's icons parsed, by name and appearance; only
// the UI thread reads and writes it.
var fileIcons = map[string]*ui.SVG{}

// fileIconSVG is an icon of the theme, for the dark appearance or the
// light one.
func fileIconSVG(icon string, dark bool) *ui.SVG {
	key := icon
	if !dark {
		key += "_light"
	}
	if svg := fileIcons[key]; svg != nil {
		return svg
	}
	b, err := fileIconFiles.ReadFile("icons/files/" + key + ".svg")
	if err != nil {
		b, err = fileIconFiles.ReadFile("icons/files/" + fileIconTheme.file + ".svg")
		if err != nil {
			panic("see: no file icon " + key)
		}
	}
	svg := ui.MustParseSVG(b)
	fileIcons[key] = svg
	return svg
}

// fileIcon draws a file's icon by its name.
func fileIcon(c *ui.Context, name string, size float32) {
	ui.Image(c, fileIconSVG(fileIconTheme.fileIconOf(name), c.Theme().Dark)).Size(size, size).Shrink(0)
}
