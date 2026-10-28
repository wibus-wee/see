# See

**See the code. Follow the idea.**

See is a desktop app for exploring source code in a focused native interface. Open a folder, navigate its files, search across the workspace, and follow symbols to their definitions—all in a native interface built with [MyGo](https://github.com/egoist/mygo), without a web view.


<img width="3146" height="2132" alt="CleanShot 2026-10-28 at 20 55 47@2x" src="https://github.com/user-attachments/assets/231c9a8b-96d5-4db0-9652-189ec76d6aa8" />


## Features

- Open multiple project folders in separate windows, browse files in tabs, and arrange editors side by side or vertically.
- Find text in a file or across a workspace, with case, whole-word, regular expression, and path filters.
- Open files quickly by fuzzy name search and navigate backward and forward through your reading history.
- Preview Markdown with native formatting, code blocks, tables, and local images, or switch back to its source.
- Explore Go, TypeScript, and JavaScript code with definitions, references, callers, diagnostics, and symbol hover information via language servers.
- Use a built-in terminal with tabs and split panes, rooted in the open workspace.
- Choose system, light, or dark appearance and adjust editor and terminal preferences.
- Reopen recent folders and see changes made to files on disk.

Edit and save UTF-8 text in the source view. Unsaved changes are marked on tabs, and See asks before closing them. If a file changes on disk, See preserves your edits and asks before overwriting it. Files shown with replacement characters remain read-only. The integrated terminal runs your shell in the workspace.

## Getting started

You'll need Go 1.27.3 or later and a graphical desktop environment. From the project directory, start the development app with live reload:

```sh
go tool mygo dev
```

Choose **Open Folder** in the welcome window. For a quick run without live reload, use `go run .` or `go run . /path/to/project`.

### Build

Build a production app for the current platform:

```sh
go tool mygo build
```

Output goes to `build/`, as configured in [`mygo.json`](./mygo.json). On macOS, `build/darwin-<arch>/` contains `See.app` and a DMG; the default signature is ad hoc. Open the app or install it from the DMG. MyGo also packages the terminal's native library and application resources. The first build may download the terminal library.

Use `go tool mygo build -platform darwin/universal` for a universal macOS build, or `go tool mygo build -h` for other targets and signing options. Creating a DMG and signing a macOS app require macOS. To compile a standalone executable for local use instead, run `go build -o see .` and start it with `./see /path/to/project`.

Release builds check for updates in the background and offer to install them. Use **See → Check for Updates…** to check manually. Development builds and installations that the current user cannot write, including Debian package installations, cannot update themselves.

Open the command palette with **Cmd+Shift+P** and choose **Shell Command: Install 'see' Command in PATH** to link the running executable at `~/.local/bin/see`. Add `~/.local/bin` to your `PATH` if needed. Use `see` or `see .` to open the current folder, `see /path/to/project` to open a folder, or `see /path/to/file` to open a file in its parent folder. The command returns immediately. Keep the app or executable at its installed location; the link points to it. Use a persistent build rather than `go run` before installing the command.

To remove the link, choose **Shell Command: Uninstall 'see' Command from PATH** in the command palette. It removes only a link pointing to the running executable.

Open another folder while a project is open to show it in a separate window. Use **File → New Window** or **Cmd+Shift+N** to start another window; closing a window checks that window for unsaved changes.

Click a file to open it as a preview; opening another file replaces that preview. Double-click the file, its tab, or its name in the path bar, or use **Keep Preview Tab** in the command palette, to keep it. Tabs appear when more than one file is open.

For a Markdown file, use the preview button in the editor bar or **Cmd+Shift+V** to switch between rendered Markdown and source. The preview uses the current buffer, including unsaved edits.

When running from source, the terminal's emulator library may be downloaded on first use.

### Optional tools

- **[ripgrep](https://github.com/BurntSushi/ripgrep)** (`rg`) enables workspace text search. Set `SEE_RG` to an absolute path or make `rg` available on `PATH`. Quick Open can list files without it.
- **[gopls](https://go.dev/gopls/)** enables Go definition lookup and symbol help. Install it with `go install golang.org/x/tools/gopls@latest`, or set `SEE_GOPLS` to its absolute path. The app also looks on `PATH` and in Go's bin directories.

- **TypeScript / JavaScript:** Install **tsrs** in **Settings → Code Intelligence**. See installs it in its own data folder using Bun, or npm if Bun is unavailable; remove it from the same page. You can instead set `SEE_TYPESCRIPT_LANGUAGE_SERVER` to the absolute path of a tsrs executable. See also looks for `tsrs` on `PATH`. Running See’s installation requires Node.js or Bun. tsrs is a TypeScript 7 implementation under active development.

The app asks you to trust a folder when you open a supported source file: servers can invoke project tools and download dependencies or toolchains. Reading files does not require trust. Server executables inside the open workspace are not used. Use **Show Output** in the command palette or select **Output** in the bottom panel to inspect server activity and errors. Output is kept in memory for the current session.

Use **Related Items** in the editor jump bar, or **Find References** and **Find Callers** in the command palette, to navigate to locations. Callers uses the language server’s incoming call hierarchy; availability depends on server support.

## Keyboard shortcuts

These are the macOS shortcuts; on other platforms, use Ctrl in place of Cmd. The full list is also available in Settings.

| Action | Shortcut |
| --- | --- |
| Command palette | Cmd+Shift+P |
| Open folder | Cmd+O |
| New window | Cmd+Shift+N |
| Save / Save all | Cmd+S / Cmd+Shift+S |
| Open Quickly | Cmd+Shift+O or Cmd+P |
| Search workspace symbols | Cmd+T (or type `#` in Open Quickly) |
| Find in workspace | Cmd+Shift+F |
| Find in file | Cmd+F |
| Toggle navigator | Cmd+B or Cmd+0 |
| Toggle Markdown preview | Cmd+Shift+V |
| Go to definition | F12 |
| Find references | Shift+F12 |
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

### Release

Set `version` in [`mygo.json`](./mygo.json), commit the change, and push a matching tag, for example:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The [release workflow](./.github/workflows/release.yml) checks the tag, builds macOS, Windows, and Linux packages and signed updates for amd64 and arm64, and publishes a GitHub Release with generated notes after all builds succeed. Versions containing a hyphen are marked as prereleases and are not offered by the default update checks. Failed jobs can be rerun from Actions; a published release is never overwritten by a rerun. The workflow uses the repository's `GITHUB_TOKEN` and requires Actions to have permission to write repository contents.

The `MYGO_UPDATER_PRIVATE_KEY` Actions secret signs updates against the public key in `mygo.json`. Keep a secure backup of the private key: existing installations cannot accept updates signed by a different key. If `CHANGELOG.md` exists, MyGo uses its section for the release version as the update window's notes and requires that section for every release. GitHub's generated release notes are separate.

The macOS packages use ad hoc signing and are not notarized, so Gatekeeper may block them on other Macs. Windows installers are unsigned and may trigger SmartScreen. Distribution without these warnings requires configuring platform signing certificates and, for macOS, Apple notarization; see [MyGo's signing instructions](https://mygo.egoist.dev/docs/github-actions).
