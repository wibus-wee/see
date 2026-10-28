# See

**See the code. Follow the idea.**

See is a desktop app for exploring source code in a focused native interface. Open a folder, navigate its files, search across the workspace, and follow Go symbols to their definitions—all in a native interface built with [MyGo](https://github.com/egoist/mygo), without a web view.

## Features

- Browse a folder in a file tree, open files in tabs, and arrange editors side by side or vertically.
- Find text in a file or across a workspace, with case, whole-word, regular expression, and path filters.
- Open files quickly by fuzzy name search and navigate backward and forward through your reading history.
- Preview Markdown with native formatting, code blocks, tables, and local images, or switch back to its source.
- Explore Go code with definition lookup and symbol hover information via `gopls`.
- Use a built-in terminal with tabs and split panes, rooted in the open workspace.
- Choose system, light, or dark appearance and adjust editor and terminal preferences.
- Reopen recent folders and see changes made to files on disk.

Edit and save text in the source view. The integrated terminal runs your shell in the workspace.

## Getting started

You'll need Go 1.27.3 or later and a graphical desktop environment. From the project directory:

```sh
go run .
```

Choose **Open Folder** in the welcome window, or pass a folder on the command line:

```sh
go run . /path/to/project
```

To build an executable:

```sh
go build -o see .
./see /path/to/project
```

Open the command palette with **Cmd+Shift+P** and choose **Shell Command: Install 'see' Command in PATH** to link the running executable at `~/.local/bin/see`. Add `~/.local/bin` to your `PATH` if needed. Then use `see` or `see .` to open the current folder, or `see /path/to/project` to open another folder. Keep the executable at its installed location; the link points to it. Build a persistent executable before installing, rather than running the app with `go run`.

Click a file to open it in a preview tab; opening another file replaces that preview. Double-click the file or its tab, or use **Keep Preview Tab** in the command palette, to keep it.

For a Markdown file, use the preview button in the editor bar or **Cmd+Shift+V** to switch between rendered Markdown and source. The preview uses the current buffer, including unsaved edits.

The terminal's emulator library may be downloaded on first use when running from source.

### Optional tools

- **[ripgrep](https://github.com/BurntSushi/ripgrep)** (`rg`) enables workspace text search. Set `SEE_RG` to an absolute path or make `rg` available on `PATH`. Quick Open can list files without it.
- **[gopls](https://go.dev/gopls/)** enables Go definition lookup and symbol help. Install it with `go install golang.org/x/tools/gopls@latest`, or set `SEE_GOPLS` to its absolute path. The app also looks on `PATH` and in Go's bin directories.

The app asks you to trust a folder before running `gopls` there: loading a Go project can invoke the Go command and download modules or toolchains. Reading files does not require trust. For both tools, an executable inside the open workspace is not used.

## Keyboard shortcuts

These are the macOS shortcuts; on other platforms, use Ctrl in place of Cmd. The full list is also available in Settings.

| Action | Shortcut |
| --- | --- |
| Command palette | Cmd+Shift+P |
| Open folder | Cmd+O |
| Open Quickly | Cmd+Shift+O or Cmd+P |
| Find in workspace | Cmd+Shift+F |
| Find in file | Cmd+F |
| Toggle Markdown preview | Cmd+Shift+V |
| Go to definition | F12 |
| Navigate back / forward | Ctrl+- / Ctrl+Shift+- |
| Add editor right / below | Cmd+D / Cmd+Shift+D |
| Switch tabs | Cmd+1–8, Cmd+9 for the last tab, Ctrl+Tab / Ctrl+Shift+Tab |
| Close tab | Cmd+W |
| Toggle terminal | Cmd+Shift+Y or Ctrl+` |
| New terminal | Ctrl+Shift+` |
| Settings | Cmd+, |

When a terminal has focus, Cmd+D splits the terminal instead of adding an editor.

## Development

Run the test suite with:

```sh
go test ./...
```

The application UI and workspace orchestration live in the root package. `internal/workspace` handles file discovery and classification, `internal/search` implements search and quick open, and `internal/lsp`, `internal/uri`, and `internal/nav` support code navigation.

Settings and recent folders are stored in `See/config.json` under the operating system's user configuration directory. On first launch after the rename, See reads the former `NativeSourceReader/config.json` if the new file does not exist; future saves use the new path. The former `NSR_RG` and `NSR_GOPLS` variables remain supported when their `SEE_` equivalents are unset.
