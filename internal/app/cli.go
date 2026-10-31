package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// openCLIPath opens what the app was started with: a folder, or a file in
// its folder.
func (a *App) openCLIPath(path string) {
	path, err := filepath.Abs(path)
	if err != nil {
		a.welcomeNote = fmt.Sprintf("Could not open %s: %v", path, describeErr(err))
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		a.welcomeNote = fmt.Sprintf("Could not open %s: %v", path, describeErr(err))
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
