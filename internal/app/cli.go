package app

import (
	"os"
	"path/filepath"

	"github.com/egoist/mygo/i18n"

	"see/locales"
)

// openCLIPath opens what the app was started with: a folder, or a file in
// its folder.
func (a *App) openCLIPath(path string) {
	path, err := filepath.Abs(path)
	if err != nil {
		a.welcomeNote = locales.Catalog.Format(i18n.Locale(), "msgCouldNotOpenSPath", map[string]any{"path": path, "reason": describeErr(err)})
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		a.welcomeNote = locales.Catalog.Format(i18n.Locale(), "msgCouldNotOpenSPath", map[string]any{"path": path, "reason": describeErr(err)})
		return
	}
	if fi.IsDir() {
		a.openWorkspace(path)
		return
	}
	root := filepath.Dir(path)
	a.openWorkspace(root)
	if a.root == root {
		a.openFile(path, nil)
	}
}

// revealCLIPath brings what `see` named forward in a window that has its
// folder open: a file opens in a tab, the folder itself only shows.
func (a *App) revealCLIPath(path string) {
	a.showWelcome = false
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		a.openFile(path, nil)
	}
}
