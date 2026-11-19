# Changelog

User-facing changes are grouped by release. Earlier releases are documented on the [Releases page](https://github.com/wibus-wee/see/releases).

## 0.3.0

### Added

- Follow the symbol around the cursor in the jump bar, Xcode style: each segment is a menu of its siblings, with Show Document Items (Control-6) and Show Group Files (Control-5) on the keyboard.
- Show a minimap of the file beside the editor, toggleable in Settings.
- Switch between a file and its counterparts, Xcode style.
- Quick Help for the symbol at the caret, plus a persistent Quick Help inspector that follows the caret and pins documentation.
- Pin a references list into a References panel that keeps it across files.
- Edit All in Scope renames every occurrence of an identifier in its scope.
- Signature help at the call site shows the current parameter as you type.
- Inlay type hints for the visible lines.
- A reading-position pin naming the declaration enclosing the caret.
- Browse incoming and outgoing calls in a Call Hierarchy panel.
- Compare the working tree against a base branch from the Git navigator, with per-file diffs and Viewed checkboxes.
- Fold unchanged stretches of a diff into expandable "N unmodified lines" separators, in working-tree diffs, comparisons, peeks and file diffs.
- Group changed files by kind — code, tests, docs, generated — with low-value groups folded.
- Nest related files in the tree (component and its tests, styles, stories) using the vscode-file-nesting-config rules.
- Show running work in the toolbar — a task, a workspace find or a starting language server — with a spinner, its name and a stop button.
- Step through a file's uncommitted changes with Next/Previous Change in file tabs, not only in diffs.
- Reveal in Navigator, Copy Path, Quick Look and Show in Finder/Explorer from the tab context menu; New Terminal Here and Open in External Terminal Here from folder rows.
- Reopen closed tabs and restore terminals across sessions.
- Set notification preferences in Settings.
- Line bookmarks with gutter marks and a Bookmarks navigator panel.
- Open Quickly and Ctrl+Tab order files by most recently used.
- File-scoped Git log, jump-to-commit commands and commit row actions in History.
- Filter projects and clone repositories on the Welcome window.
- Configure additional language servers in config.json.
- `see` CLI flags for opening a diff, reading stdin and forcing a new window.
- Middle-truncate long names — files, branches, breadcrumbs, tabs — instead of clipping them.

### Fixed

- Go Back and Go Forward reuse a tab already open in another editor group instead of opening a duplicate.
- Drop the minimap shortcut that collided with Show Problems off macOS.

## 0.2.5

### Added

- Find and replace text in the current file with Replace in File, or across the workspace from the Find navigator with per-match previews and a confirmed Replace All.
- Browse, filter and jump to the current file's symbols in the Outline navigator.
- Go to Implementation and Go to Type Definition from the editor through the language server.
- Fold and unfold code by block, recursively, by level or all at once, optionally fold imports when opening a file, and keep folds across sessions.
- Show the current line's Git blame beside the code and reveal its commit in Git Log; turn inline blame off in Settings or from the command palette.
- Show errors and warnings at the end of their lines with a subtle line tint; turn inline diagnostics off in Settings.

### Fixed

- Stop console windows from flashing on Windows when See runs Git, language servers, search and other tools.
- Open the Welcome window when See starts on Windows, and install the `see` shell command as a launcher that works from PowerShell and Command Prompt.
- Install ripgrep on Windows when its release publishes CertUtil-format checksum files.
- Show relative dates in Git blame and the branch picker in the selected language.

## 0.2.4

### Added

- Preview local videos with native playback controls, and open saved files in Quick Look from the macOS file navigator.
- Create untitled text files with Cmd+N, including from the Welcome screen.

## 0.2.3

### Added

- Browse Git history in an aligned branch graph table with separate branch or tag, graph, commit message, author, date and hash columns; filter commits, load older history and horizontally scroll dense graphs.

### Fixed

- Keep remote Git status current by fetching when a repository opens and periodically afterward, so incoming and outgoing counts refresh without a manual fetch.
- Show upstream-only commits in History and Git Log with incoming and outgoing markers, and refresh history when external pushes, fetches or tags change repository refs.

## 0.2.2

### Fixed

- Match Quick Open queries against directories and filenames independently, preserve directory order in path queries, and highlight matching folder names.
- Report file-discovery failures instead of showing partial results as a successful scan.
- Preserve newly edited text during bulk tab-close confirmation, and close saved untitled tabs correctly after Save As.
- Transfer keyboard focus correctly between text, Markdown, image, diff, hexadecimal and empty editor groups.

### Added

- Jump to a line or column with Ctrl+G, search declarations in the current file with `@` in Open Quickly, and navigate current-file diagnostics with F8 or Shift+F8.
- Browse and filter diagnostics from open workspace files in the Problems navigator with Cmd+Shift+M.
- Run Go build and test tasks in trusted workspaces, stop running tasks, and jump to source locations from their output.
- Close other, right-hand or saved tabs from tab context menus, cycle editor groups with F6 or Shift+F6, and move the active tab to the next editor from the command palette.

## 0.2.1

### Added

- Use See in Simplified Chinese, including the command palette and language settings.
- Highlight CMake files and use clangd or cmake-language-server for code navigation and diagnostics in trusted folders.
- Install or uninstall the `see` shell command from Settings.
- Hide selected file and folder names in the project navigator; `.DS_Store` is hidden by default.

### Fixed

- Dim navigator entries based on Git ignore status, and exclude hidden names from navigator filtering.

## 0.2.0

### Changed

- See has a new app icon and refreshed visual identity.

## 0.1.18

### Added

- Open Git history in a dedicated log tab, filter commits, and view a commit's file diffs together.
- Added a project website with features, installation information, and download links.

## 0.1.17

### Added

- Open recent projects from the macOS Dock menu.
- Stage or unstage all Git changes from the Changes header.

### Fixed

- New windows show the Welcome screen instead of an empty editor.
- Shift-click selects a range of Git changes, and selections survive status refreshes.

## 0.1.16

### Fixed

- Closing Welcome without a folder no longer leaves an empty editor window open.
- Files dropped onto the macOS Dock icon open as files, and folders open as projects.
- Folderless windows no longer show a navigator button without a sidebar.
