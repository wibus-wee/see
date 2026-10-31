# See

**See the code. Follow the idea.**

See is a desktop app for reading source code. Open a folder and you get its tree, tabs, search, symbols, diffs, and a terminal in one window — written in Go and built with [MyGo](https://github.com/egoist/mygo), drawn natively, with no web view in the stack.


<img width="3146" height="2132" alt="CleanShot 2026-10-28 at 20 55 47@2x" src="https://github.com/user-attachments/assets/231c9a8b-96d5-4db0-9652-189ec76d6aa8" />


## Install

Download the latest build from [Releases](https://github.com/wibus-wee/see/releases/latest).

| Platform | Package |
| --- | --- |
| macOS | `.dmg` for Apple silicon and Intel |
| Windows | `-setup-.exe` for x64 and arm64 |
| Linux | `.deb` or `.tar.gz` for amd64 and arm64 |

The macOS builds are ad hoc signed and not notarized, so Gatekeeper may block them on other Macs. The Windows installers are unsigned and may trigger SmartScreen.

Release builds check for updates in the background and offer to install them; use **See → Check for Updates…** to check manually. Development builds, and installations the current user cannot write to — including Debian package installations — cannot update themselves.

### Build from source

You'll need Go 1.27.3 or later and a graphical desktop environment. From the project directory, start the development app with live reload:

```sh
go tool mygo dev
```

Choose **Open Folder** in the welcome window. For a quick run without live reload, use `go run .` or `go run . /path/to/project`.

Build a production app for the current platform:

```sh
go tool mygo build
```

Output goes to `build/`, as configured in [`mygo.json`](./mygo.json). On macOS, `build/darwin-<arch>/` contains `See.app` and a DMG. MyGo also packages the terminal's native library and application resources; the first build may download that library.

Use `go tool mygo build -platform darwin/universal` for a universal macOS build, or `go tool mygo build -h` for other targets and signing options. Creating a DMG and signing a macOS app require macOS. To compile a standalone executable for local use instead, run `go build -o see .` and start it with `./see /path/to/project`.

### The `see` shell command

Open the command palette with **Cmd+Shift+P** and choose **Shell Command: Install 'see' Command in PATH** to link the running executable at `~/.local/bin/see`; add `~/.local/bin` to your `PATH` if needed. The command returns immediately and the link points at the app or executable in place, so use a persistent build rather than `go run`.

Use `see` or `see .` for the current folder, `see /path/to/project` to open a folder, or `see /path/to/file` to open a file in its parent folder. To remove the link, choose **Shell Command: Uninstall 'see' Command from PATH**; it removes only a link pointing to the running executable.

## Features

- **Read** — open several folders in separate windows, keep files in tabs, and arrange editors side by side or one below another.
- **Search** — find text in the current file or across the workspace, with case, whole-word, regular expression, and path filters.
- **Jump** — reach files by fuzzy name, search workspace symbols, and step back and forward through your reading history.
- **Understand** — definitions, references, callers, diagnostics, and hover help for Go, TypeScript, and JavaScript, through language servers.
- **Review** — stage, unstage, commit, and discard changes, open a change as a diff tab, and see each file's status beside it in the navigator.
- **Preview** — Markdown rendered natively, and any file as hexadecimal bytes when text is not an option.
- **Run** — a terminal with tabs and split panes, rooted in the open workspace.
- **Settle in** — system, light, or dark appearance, editor and terminal preferences, recent folders, and a notice when files change on disk.

## Working with files

### Windows, tabs, and previews

Opening another folder while a project is open shows it in a separate window. Use **File → New Window** or **Cmd+Shift+N** to start one; closing a window checks that window for unsaved changes.

Clicking a file opens it as a preview, and opening another file replaces that preview. Double-click the file, its tab, or its name in the path bar, or use **Keep Preview Tab** in the command palette, to keep it. Tabs appear once more than one file is open.

Rename a file or folder from its context menu. On macOS, select an item in the file navigator and press Return to rename it; press Escape to cancel.

### Editing, saving, and recovery

Edit and save UTF-8 text in the source view. Unsaved changes are marked on tabs, and See asks before closing them. If a file changes on disk, See preserves your edits and asks before overwriting it. Files shown with replacement characters remain read-only.

See caches unsaved text for open files so it can restore edits after an unexpected exit. If the disk version changed in the meantime, it preserves the recovered edits and asks before overwriting.

### Markdown preview

For a Markdown file, use the preview button in the editor bar or **Cmd+Shift+V** to open a preview tab, and drag that tab to an editor edge to read it beside the source. The preview uses the current buffer, so unsaved edits show up in it.

Images in their own paragraph can use Markdown image syntax or a standalone HTML `<img>` tag. Remote HTTP and HTTPS images are fetched when the preview opens, so their host can see the request. Other HTML blocks display as source.

### Binary files

Binary files open as a read-only hexadecimal view with offsets and printable ASCII, including files too large to open as text. Use **Show File Bytes** in the editor bar to inspect the bytes of any saved file; the view reads from disk, so unsaved edits are not shown there.

Click a byte and Shift-click, or hold Shift with the arrow keys, to select a range. **Copy Hex** copies up to 1 MiB. Enter a hexadecimal offset to jump, or search for text or a byte sequence prefixed with `hex:` — for example, `hex: 00 FF`.

## Code intelligence

Language support comes from external servers. See installs and manages the ones it knows about, and uses anything you already have:

| Tool | Enables | Where it comes from |
| --- | --- | --- |
| [ripgrep](https://github.com/BurntSushi/ripgrep) (`rg`) | workspace text search | **Settings → Search**, `SEE_RG`, or `PATH` |
| [gopls](https://go.dev/gopls/) | Go definitions and hover help | **Settings → Code Intelligence**, `SEE_GOPLS`, `PATH`, or Go's bin directories |
| **tsrs** | TypeScript and JavaScript support | **Settings → Code Intelligence**, `SEE_TYPESCRIPT_LANGUAGE_SERVER`, or `PATH` |

Tools you install from Settings live in See's application data folder (`~/Library/Application Support/See` on macOS) and are never written into the workspace. See downloads ripgrep's release for your platform and checks it against the published SHA-256, builds gopls with your Go toolchain — so Go must be installed and on `PATH` first — and installs tsrs with Bun, or npm if Bun is unavailable. A copy See installed is used before one on `PATH`; remove it in Settings to go back to your own. Quick Open lists files without ripgrep.

tsrs is a TypeScript 7 implementation under active development, so expect it to move.

The app asks you to trust a folder when you open a supported source file: a server can invoke project tools and download dependencies or toolchains. Reading files does not require trust, and server executables inside the open workspace are not used. Use **Show Output** in the command palette, or **Output** in the bottom panel, to inspect server activity and errors; output is kept in memory for the current session.

Use **Related Items** in the editor jump bar, or **Find References** and **Find Callers** in the command palette, to move between locations. Callers uses the language server's incoming call hierarchy, so availability depends on the server.

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
| Open Markdown preview | Cmd+Shift+V |
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

The application UI and workspace orchestration live in the root package. `internal/workspace` handles file discovery and classification, `internal/search` implements search and Quick Open, `internal/git` reads repository state and diffs, and `internal/lsp`, `internal/uri`, and `internal/nav` support code navigation.

Settings and recent folders are stored in `See/config.json` under the operating system's user configuration directory.

### Release

Set `version` in [`mygo.json`](./mygo.json), commit the change, and push a matching tag, for example:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The [release workflow](./.github/workflows/release.yml) checks the tag, builds macOS, Windows, and Linux packages and signed updates for amd64 and arm64, and publishes a GitHub Release with generated notes after all builds succeed. The body starts with a link to each platform's installer, since the asset list below it also holds the delta files that installed copies use to update themselves. Versions containing a hyphen are marked as prereleases and are not offered by the default update checks. Failed jobs can be rerun from Actions; a published release is never overwritten by a rerun. The workflow uses the repository's `GITHUB_TOKEN` and requires Actions to have permission to write repository contents.

The `MYGO_UPDATER_PRIVATE_KEY` Actions secret signs updates against the public key in `mygo.json`. Keep a secure backup of the private key: existing installations cannot accept updates signed by a different key. If `CHANGELOG.md` exists, MyGo uses its section for the release version as the update window's notes and requires that section for every release. GitHub's generated release notes are separate.

The macOS packages use ad hoc signing and are not notarized, so Gatekeeper may block them on other Macs. Windows installers are unsigned and may trigger SmartScreen. Distribution without these warnings requires configuring platform signing certificates and, for macOS, Apple notarization; see [MyGo's signing instructions](https://mygo.egoist.dev/docs/github-actions).
