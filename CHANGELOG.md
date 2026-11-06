# Changelog

User-facing changes are grouped by release. Earlier releases are documented on the [Releases page](https://github.com/wibus-wee/see/releases).

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
