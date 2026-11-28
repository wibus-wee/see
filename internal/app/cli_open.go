package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"see/internal/cli"
	"see/internal/config"
	"see/internal/git"
	"see/internal/workspace"
	"see/locales"

	"github.com/egoist/mygo/ui/editor"
)

// openRequest opens what a `see` invocation asked of the app: the diff
// of two paths, the standard input as an untitled document, or a path.
func (a *App) openRequest(req cli.Request) {
	switch {
	case req.Diff[0] != "":
		a.showCliDiff(req.Diff[0], req.Diff[1])
	case req.Stdin:
		a.openStdin(req.Path)
	default:
		a.openCLIPath(req.Path)
	}
}

// revealRequest brings forward what a `see` invocation asked while the
// app runs, in the window it belongs to.
func (a *App) revealRequest(req cli.Request) {
	switch {
	case req.Diff[0] != "":
		a.showCliDiff(req.Diff[0], req.Diff[1])
	case req.Stdin:
		a.openStdin(req.Path)
	default:
		a.revealCLIPath(req.Path)
	}
}

// openStdin reads what the invocation piped in into an untitled
// document, marked so it asks before closing, as one typed does. The
// file it was kept in is taken once read; the document's own draft
// cache, in the folder untitled ones use, keeps it from then on.
func (a *App) openStdin(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.fail(locales.CouldNotReadStdin() + describeErr(err))
		return
	}
	_ = os.Remove(path) // taken once read
	draft, err := stdinDraftPath(data)
	if err != nil {
		a.fail(locales.MsgCouldNotCreateRecoveryCache() + err.Error())
		return
	}
	if a.root == "" && len(a.docs) == 0 {
		a.host.SetWorkspaceSize(config.DefaultWindowWidth, config.DefaultWindowHeight)
	}
	buf := editor.NewBuffer("")
	buf.Set(string(data))
	buf.SetLanguage(languageFor("stdin", string(data)))
	d := &doc{path: draft, uri: uriOf(draft), name: a.untitledName(), untitled: true, cache: &draftCache{}, buf: buf, kind: workspace.Text}
	a.docs[draft] = d
	a.openFile(draft, nil)
	a.focusEditor = a.focus.eds[draft]
	a.welcomeNote = ""
	a.showWelcome = false
	a.saveConfig()
}

// stdinDraftPath is the draft-cache path a document of piped input is
// kept at, created with that input, as newFile's is.
func stdinDraftPath(data []byte) (string, error) {
	dir, err := draftDir()
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	for {
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return "", err
		}
		path := filepath.Join(dir, fmt.Sprintf("%x", id))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(path)
			return "", werr
		}
		return path, nil
	}
}

// cliDiffPrefix is the key of a diff `see --diff` opened; the two paths
// after it make each pair its own tab.
const cliDiffPrefix = diffTabPrefix + "cli:"

func cliDiffTab(old, new string) string {
	return cliDiffPrefix + old + "\x00" + new
}

// showCliDiff shows what `see --diff` compares in a tab of its own:
// directories are compared with git's --no-index diff, files with the
// diff viewer's own comparison.
func (a *App) showCliDiff(old, new string) {
	a.showWelcome = false
	if a.root == "" {
		// A folderless window starts welcome-sized; a diff asks for the
		// room a workspace gets.
		a.host.SetWorkspaceSize(config.DefaultWindowWidth, config.DefaultWindowHeight)
	}
	oldAbs, err := canonicalDiffPath(old)
	if err != nil {
		a.fail(locales.MsgCouldNotOpen() + old + ": " + describeErr(err))
		return
	}
	newAbs, err := canonicalDiffPath(new)
	if err != nil {
		a.fail(locales.MsgCouldNotOpen() + new + ": " + describeErr(err))
		return
	}
	if isDir(oldAbs) && isDir(newAbs) {
		a.showDirDiff(oldAbs, newAbs)
		return
	}
	oldText, err := readDiffFile(oldAbs)
	if err != nil {
		a.fail(locales.MsgCouldNotOpen() + filepath.Base(oldAbs) + ": " + describeErr(err))
		return
	}
	newText, err := readDiffFile(newAbs)
	if err != nil {
		a.fail(locales.MsgCouldNotOpen() + filepath.Base(newAbs) + ": " + describeErr(err))
		return
	}
	g := a.focus
	key := cliDiffTab(oldAbs, newAbs)
	oldBuf, newBuf := editor.NewBuffer(oldText), editor.NewBuffer(newText)
	oldBuf.SetLanguage(languageFor(filepath.Base(oldAbs), oldText))
	newBuf.SetLanguage(languageFor(filepath.Base(newAbs), newText))
	v := a.newCLIBufferDiff(oldBuf, newBuf, diffName(oldAbs, newAbs))
	if g.diffs == nil {
		g.diffs = map[string]*gitDiffTab{}
	}
	g.diffs[key] = v
	g.addTab(key, len(g.order))
	a.activateIn(g, key)
}

func (a *App) newCLIBufferDiff(old, new *editor.Buffer, name string) *gitDiffTab {
	d := &editor.DiffState{Theme: a.edTheme}
	setBufferDiffWhitespace(d, old, new, a.cfg.HideWhitespaceChanges)
	a.applyDiffPrefs(d)
	return &gitDiffTab{state: d, oldBuffer: old, newBuffer: new, name: name, cli: true}
}

// showDirDiff puts the changes git's --no-index diff finds between two
// folders in a diff tab a file, each named for where it is in them.
func (a *App) showDirDiff(old, new string) {
	gen, ctx := a.gen, a.wsCtx
	post := a.post.Post
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		out, err := dirDiff(ctx, old, new)
		post(func() {
			if a.gen != gen {
				return
			}
			if err != nil {
				a.fail(locales.MsgDiff() + gitErrLine(err))
				return
			}
			patches, err := editor.ParsePatch(string(out))
			if err != nil || len(patches) == 0 {
				a.fail(locales.MsgNoTextDiffAvailableFor() + old)
				return
			}
			g := a.focus
			if g.diffs == nil {
				g.diffs = map[string]*gitDiffTab{}
			}
			var first string
			for _, p := range patches {
				if p.Binary {
					continue
				}
				name := p.NewName
				if name == "" {
					name = p.OldName
				}
				key := cliDiffTab(filepath.Join(old, p.OldName), filepath.Join(new, p.NewName))
				if first == "" {
					first = key
				}
				v := a.newGitDiffTab(p, name, false)
				v.cli = true
				g.diffs[key] = v
				g.addTab(key, len(g.order))
			}
			if first == "" {
				a.fail(locales.MsgNoTextDiffAvailableFor() + old)
				return
			}
			a.activateIn(g, first)
		})
	}()
}

// dirDiff is the unified diff of two folders, from git's --no-index
// diff, which shows added and removed files too; no repository is
// needed. A difference is exit 1, not an error.
func dirDiff(ctx context.Context, old, new string) ([]byte, error) {
	out, err := git.Command(ctx, ".", "diff", "--no-index", "--no-ext-diff", "--no-color", "--unified=2147483647", "--", old, new)
	if len(out) > 0 {
		err = nil
	}
	return out, err
}

// canonicalDiffPath resolves what a diff's side names, as Openable did
// for the shell: an existing file or folder, absolute and symlink-free.
func canonicalDiffPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		return "", fmt.Errorf("expected a file or folder")
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	return abs, nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// readDiffFile is the text a file brings to a diff: UTF-8 text, its
// replacement characters for other encodings; a binary file has none.
func readDiffFile(path string) (string, error) {
	f, err := workspace.ReadFile(path)
	if err != nil {
		return "", err
	}
	switch f.Kind {
	case workspace.Text:
		return f.Text, nil
	case workspace.BadEncoding:
		if f = readLossy(path, f); f.Kind == workspace.Text {
			return f.Text, nil
		}
		return "", errors.New(locales.BadEncodingDetail())
	case workspace.TooLarge:
		return "", errors.New(locales.FileIsTooLargeToShow(filepath.Base(path)))
	default:
		return "", errors.New(locales.IsNotText(filepath.Base(path)))
	}
}

// diffName is what a diff tab of two files is called.
func diffName(old, new string) string {
	if filepath.Base(old) == filepath.Base(new) {
		return filepath.Base(old)
	}
	return filepath.Base(old) + " ↔ " + filepath.Base(new)
}
