# Changelog

User-facing changes are grouped by release. Earlier releases are documented on the [Releases page](https://github.com/wibus-wee/see/releases).

## 0.3.12

### Added

- Open and edit VS Code `.code-workspace` files with multiple folders after enabling Multi-root Workspaces in Settings → Beta Features; root names, exclusions, search, language servers, Git repositories, watchers, and restored sessions follow each workspace folder.
- Manage unfinished features in Settings → Beta Features, including experimental Sidebar Vibrancy with a choice of system materials.

### Changed

- Show the same commit subject, body, co-authors, hash, refs, signature key, and file diffs in commit tabs and Git Log, with folding and the whitespace preference in both places.
- Fit Git Log columns to their content, keeping long branch names, authors, hashes, and timestamps readable.

### Fixed

- Keep editor groups opaque over the window material, so split editors do not show the desktop or windows behind them.

## 0.3.11

### Added

- Discover nested Git repositories in immediate child folders when enabled; Changes can group their files with repository-scoped diffs, staging, discarding, and commit composers while Files and Search stay rooted at the workspace.
- Switch working-copy changes between a collapsible folder tree and a flat list, with section-level stage, unstage, and confirmed discard actions.
- Merge project windows into native macOS window tabs with Window → Merge All Windows while keeping each project's editor state separate.
- Choose whether full-file diffs fold unchanged lines, from the diff toolbar or General settings.

### Changed

- Resize Call Hierarchy's source preview and call list with a draggable divider.
- Use the desktop portal file chooser on Linux when available, while preserving explicit `GTK_USE_PORTAL` settings.

### Fixed

- Keep the current Jump Bar symbol visible on deep paths; collapsed crumbs move into an overflow menu in narrow panes.
- Replace transient previews without loading flashes or layout jumps, keeping the outgoing preview visible until its replacement is ready.
- Remove mixed-language and untranslated copy across the Git sidebar, commit views, dates, link actions, empty states, and tab commands.

## 0.3.10

### Added

- Open an editor context menu for navigation, symbol actions, formatting, editing, history and comparison, copying, and sharing.
- Use the Regex Playground as a movable, resizable floating window that can span editor panes and remembers its geometry per workspace.
- Copy host permalinks from commit file diffs and revision comparisons, pinned to the commit being read; lines selected on the diff's new side become the link anchor.

### Changed

- Lead workspace trust prompts with the decision, explain what language servers may do, and make inspector tabs and empty states clearer.

### Fixed

- Keep inlay type and parameter hints anchored through edits instead of letting them vanish or drift while the language server catches up.
- Correct Git log details: show incoming and unpushed arrows only on genuine mainline commits, count the full history exactly, keep Load More clear of rows, middle-truncate long tags, and back off automatic fetches while offline.
- Give icon-only controls, toggles, fields, placeholders, menus, and tips localized accessible names and states.

## 0.3.9

### Added

- Hover a line's trailing inline blame to see the commit author, date, message, and hash in a quiet Quick Help-style card; choose the hash to open the commit in Git Log. The card also works in plain-text files and untrusted workspaces.
- Find commands in the palette by their stable dotted id or English name in any UI language, and see the dotted id under each command title.

### Fixed

- Select and preview the chosen call when clicking a row in Call Hierarchy.
- Show the editor's Cmd-hover link immediately while holding Cmd, rather than waiting for a language-server hover response.
- Keep inlay type and parameter hints visible while scrolling instead of flickering off until each new language-server response.
- Apply the editor line-height preference to row layout, including after preference resets and across workspace windows.
- On Linux, show the launcher when See starts from the desktop and avoid quitting the app when switching projects from the titlebar menu.

## 0.3.8

### Added

- Follow relative links in Markdown previews and Cmd+click links in Markdown source, opening the target file at the line or heading named by the fragment.

### Changed

- Use a clearer glass material for popovers and drawn picker menus, so content behind them reads through.
- Show file-type icons in the editor language menu.
- Configure wrapping for all languages in a single per-language list in Editor settings.
- Flatten inspector sections into Xcode-style rows separated by hairlines instead of boxed cards.

### Fixed

- Keep navigator icons at fixed spacing in narrow sidebars; the selector scrolls horizontally without a visible scrollbar.

## 0.3.7

### Added

- Get language-server completions as you type or request them with Ctrl+Space, including property suggestions and candidate details. Prefix edits refresh suggestions; unsupported snippets, commands, additional edits, and incompatible replacement ranges are omitted rather than partially applied.
- Detect document languages from filenames and content and choose a language from the editor status bar; manual selections remain in effect through edits, reloads, saves, and renames.
- Configure soft wrapping by language in Editor settings. Default follows the global Wrap Lines setting, while Wrap and Don't Wrap override it.

### Changed

- Streamline the Git sidebar with compact Working Copy/Repositories selectors, flat Git-ordered change lists, and single-line stashes. Keep the commit composer in one pill with its inline action, prioritize filenames before directory paths, and show full paths—including both sides of renames—in comparison tooltips.

### Fixed

- Show signature help in a content-sized popup, keep the active parameter current, and suppress it in nested object or array literals and callback bodies.
- Refresh completion suggestions after Backspace and IME composition commits. Resolve completion details even when documentation and label details are already present, preferring the resolved main detail while retaining the label-detail fallback when no main detail is returned.
- Refresh wrapping preferences after a document's first save, apply them to CMake patch diffs, and update cached inline Git previews when language-specific preferences change.

## 0.3.6

### Fixed

- Fix editors cached inside popups and panels — code previews in the pinned references list and call hierarchy, Peek Definition, the merge-conflict panes, the regex playground sample, and the workspace search-and-replace editor — keeping the old appearance after the system or chosen theme changes, so they could draw light inside a dark window (or the reverse) until reopened.

## 0.3.5

### Added

- Reveal the active file in Project Navigator from the keyboard, command palette, or tab menu; the command opens Files, clears its filter, expands the file's folders, and selects it.
- Show whitespace characters in the editor and highlight occurrences of the symbol under the caret, with a stronger tint for writes when the language server distinguishes them.
- Maximize the focused editor in split layouts, change editor font size with commands, and move to the next or previous method.
- Keep enclosing declarations pinned while scrolling and configure an editor page guide.
- Read documentation comments inline with code, emphasis, links, and parameter labels. Move the caret into a comment to edit its source, or disable Render Doc Comments in Editor settings or with Toggle Reader Mode.
- Browse Type Hierarchy with expandable Supertypes and Subtypes trees, code previews, and navigation for language servers that support it.
- Save Find navigator queries, edit workspace search results in place, and open file symbols from Open Quickly with `file@symbol`.
- View workspace diagnostics in the Problems navigator.
- Navigate repositories and use commit actions in the Git navigator; stage or unstage selected hunks and lines, and discard a change from its preview.
- Compare files against past commits in Code Review and ignore whitespace in diffs.
- Resolve merge conflicts in See's native conflict editor.
- Create and browse Git stashes, then apply or drop them, including stashes with staged, unstaged, and untracked changes.

### Changed

- Improve file search and editor scrolling in large workspaces.

### Fixed

- Fix first-run issues involving F12 navigation, unsaved-file close prompts, cloning, trust handling, and command-palette use with split editors.

## 0.3.4

### Added

- Test regular expressions in an inline playground with sample text, match and capture inspection, and a railroad diagram. The playground uses Go / RE2 and reports unsupported JavaScript flags and features without changing source code.
- Preview hex, RGB(A), and HSL(A) color literals with inline swatches; click a swatch to compare the color against light and dark backgrounds.
- Browse local file history in a diff tab and restore selected changed lines with click and Shift-click. Recovery preserves other edits, does not save automatically, and can be undone in the source editor.
- Preview a branch's three latest commits and affected open files from the toolbar branch picker before switching. Comparisons use committed files and mark unsaved buffers without changing the current branch.

### Changed

- Hold Option on macOS or Ctrl+Alt on Windows/Linux to reveal parameter-name hints from the language server; release the keys to hide them. Parameter hints remain available independently of the type-hint preference.

## 0.3.3

### Changed

- Parameter Help now defaults to On Trigger: the signature strip appears only on Control-Shift+Space instead of whenever the caret rests inside a call, matching Xcode's opt-in behavior; Always remains available in Settings.

## 0.3.2

### Added

- Xcode-style inspector tabs on the right: a segmented strip switches between a File inspector (identity, info, location, text settings, Git status), a History inspector (recent commits touching the file), and Quick Help.
- Semantic highlighting from the language server, layered over the tokenizer — calls, types and enum members color by meaning where the server supports it.
- Cmd+click document links: imports, URLs and file references offered by the language server open the file or the browser.
- Code actions: a lightbulb menu offers the fixes and refactorings a server provides and applies their workspace edits.
- Rename a symbol across the workspace with F2, previewed through the language server.
- Format Document and Format Selection through the language server.
- Expand Selection / Shrink Selection step through the syntax ranges the server reports.
- Fold ranges come from the language server when it provides them.

### Fixed

- A race in the app scheduler that could drop or reorder posted work.

## 0.3.1

### Fixed

- Keep the Welcome window's action list from overflowing onto the "Show this window when See launches" checkbox, and keep its recent-folder rows clear of the window buttons on Linux and Windows.
- Move the inspector toggle from the jump bar to the toolbar, tighten the jump bar's right-side buttons so their hover background stays inside the bar, and center the symbol kind letters in their chips.
- Show a chevron separator after the reading pin's collapsed "…", matching the jump bar.

### Removed

- The "Filter folders" field on the Welcome window.

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
