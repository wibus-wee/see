package app

import (
	"github.com/egoist/mygo/i18n"
	"see/locales"
	"strconv"
	"strings"
)

var commandMessages = map[string]string{
	"Show Problems":              "commandShowProblems",
	"Search File Symbols":        "commandSearchFileSymbols",
	"Go: Build Workspace":        "commandGoBuild",
	"Go: Test Workspace":         "commandGoTest",
	"Tasks: Show":                "commandShowTasks",
	"Tasks: Stop":                "commandStopTasks",
	"Next Problem":               "commandNextProblem",
	"Previous Problem":           "commandPreviousProblem",
	"Go to Line":                 "commandGoToLine",
	"New File":                   "msgNewFile2",
	"Command Palette":            "commandCommandPalette",
	"New Window":                 "msgNewWindow",
	"Open Folder":                "msgOpenFolder2",
	"Open Quickly":               "msgOpenQuickly",
	"Search Workspace Symbols":   "commandSearchWorkspaceSymbols",
	"Open Markdown Preview":      "commandOpenMarkdownPreview",
	"Find in Workspace":          "commandFindInWorkspace",
	"Show Git":                   "commandShowGit",
	"Git: Show History":          "commandGitShowHistory",
	"Git: Show Log":              "commandGitShowLog",
	"Toggle Inline Diff":         "commandToggleInlineDiff",
	"Next Change":                "commandNextChange",
	"Previous Change":            "commandPreviousChange",
	"Git: Fetch":                 "commandGitFetch",
	"Git: Pull":                  "commandGitPull",
	"Git: Push":                  "commandGitPush",
	"Git: Switch Branch…":        "commandGitSwitchBranch",
	"Filter Files":               "commandFilterFiles",
	"Show or Hide the Navigator": "commandShowOrHideTheNavigator",
	"Go Back":                    "commandGoBack",
	"Go Forward":                 "commandGoForward",
	"Next Tab":                   "commandNextTab",
	"Previous Tab":               "commandPreviousTab",
	"Save":                       "msgSave",
	"Save All":                   "commandSaveAll",
	"Close Tab":                  "commandCloseTab",
	"Add Editor on Right":        "commandAddEditorOnRight",
	"Add Editor Below":           "commandAddEditorBelow",
	"Show or Hide the Terminal":  "commandShowOrHideTheTerminal",
	"Show Output":                "commandShowOutput",
	"New Terminal":               "commandNewTerminal",
	"Open in External Terminal":  "commandOpenInExternalTerminal",
	"Settings":                   "settings",
	"Keep Preview Tab":           "commandKeepPreviewTab",
	"Shell Command: Install 'see' Command in PATH":     "commandShellCommandInstallSeeCommandInPath",
	"Shell Command: Uninstall 'see' Command from PATH": "commandShellCommandUninstallSeeCommandFromPath",
	"Find in File":     "commandFindInFile",
	"Find References":  "commandFindReferences",
	"Find Callers":     "commandFindCallers",
	"Go to Definition": "commandGoToDefinition",
	"Select Last Tab":  "commandSelectLastTab",
}

func commandLabel(name string) string {
	if suffix, ok := strings.CutPrefix(name, "Select Tab "); ok {
		if n, err := strconv.Atoi(suffix); err == nil {
			return locales.CommandSelectTab(n)
		}
	}
	if key := commandMessages[name]; key != "" {
		return locales.Catalog.Format(i18n.Locale(), key, nil)
	}
	return name
}
