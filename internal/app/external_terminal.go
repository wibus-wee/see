package app

import (
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
)

var externalTerminalNames = []string{"Terminal", "Ghostty", "Rex", "Seedmux", "Custom App"}

var externalTerminalIDs = map[string]string{
	"Terminal": "com.apple.Terminal", "Ghostty": "com.mitchellh.ghostty",
	"Rex": "com.superlogical.rex", "Seedmux": "com.xx.seedmux",
}

func externalTerminalName(name string) string {
	if slices.Contains(externalTerminalNames, name) {
		return name
	}
	return "Terminal"
}

// Seedmux exposes its folder-opening action as a macOS service. Pass the
// directory on a private pasteboard so paths never become script source.
const seedmuxService = `ObjC.import("AppKit");
function run(argv) {
  $.NSApplication.sharedApplication;
  $.NSUpdateDynamicServices();
  const p = $.NSPasteboard.pasteboardWithUniqueName;
  p.declareTypesOwner(["NSFilenamesPboardType"], null);
  p.setPropertyListForType([argv[0]], "NSFilenamesPboardType");
  if (!$.NSPerformService("New Seedmux Tab Here", p))
    throw Error("Seedmux's folder service is unavailable. Make sure Seedmux is installed.");
}`

func externalTerminalCommand(name, appPath, dir, platform string) (*exec.Cmd, error) {
	if platform != "darwin" {
		return nil, fmt.Errorf("external terminal apps are currently supported on macOS")
	}
	switch externalTerminalName(name) {
	case "Custom App":
		if strings.TrimSpace(appPath) == "" {
			return nil, fmt.Errorf("choose an external terminal app in Settings first")
		}
		return exec.Command("open", "-a", appPath, dir), nil
	case "Seedmux":
		return exec.Command("osascript", "-l", "JavaScript", "-e", seedmuxService, "--", dir), nil
	case "Ghostty":
		return exec.Command("open", "-b", "com.mitchellh.ghostty", dir), nil
	case "Rex":
		return exec.Command("open", "-b", "com.superlogical.rex", dir), nil
	default:
		return exec.Command("open", "-b", "com.apple.Terminal", dir), nil
	}
}

func (a *App) openExternalTerminal() {
	if a.root == "" {
		a.note("Open a folder first.", true)
		return
	}
	name, dir, gen := externalTerminalName(a.cfg.ExternalTerminal), a.root, a.gen
	cmd, err := externalTerminalCommand(name, a.cfg.ExternalTerminalPath, dir, runtime.GOOS)
	if err != nil {
		a.note(err.Error(), true)
		return
	}
	go func() {
		out, err := cmd.CombinedOutput()
		if err != nil {
			detail := strings.TrimSpace(string(out))
			if detail == "" {
				detail = err.Error()
			}
			a.post.Post(func() {
				if a.gen == gen {
					a.note("Could not open "+name+": "+detail, true)
				}
			})
		}
	}()
}
