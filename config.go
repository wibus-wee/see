package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
)

// config is what the app remembers between runs.
type config struct {
	LastWorkspace        string                      `json:"lastWorkspace,omitempty"`
	Recent               []string                    `json:"recentWorkspaces,omitempty"`    // newest first
	ReopenLast           bool                        `json:"reopenLastWorkspace,omitempty"` // else the welcome window shows at launch
	SidebarWidth         float32                     `json:"sidebarWidth,omitempty"`
	Appearance           string                      `json:"appearance,omitempty"` // "light", "dark", or "" for the system's
	Trusted              []string                    `json:"trustedWorkspaces,omitempty"`
	FontFamily           string                      `json:"editorFontFamily,omitempty"`
	TermFontFallback     string                      `json:"terminalFontFallback,omitempty"`
	TermFontFamily       string                      `json:"terminalFontFamily,omitempty"`
	ExternalTerminal     string                      `json:"externalTerminal,omitempty"`
	ExternalTerminalPath string                      `json:"externalTerminalPath,omitempty"`
	TermFontWeight       int                         `json:"terminalFontWeight,omitempty"`
	TermLineHeight       float64                     `json:"terminalLineHeight,omitempty"`
	TermThicken          bool                        `json:"terminalFontSmoothing,omitempty"`
	TermNoLigatures      bool                        `json:"terminalNoLigatures,omitempty"`
	FontSize             float64                     `json:"editorFontSize,omitempty"`    // 13 when 0
	LineNumbers          int                         `json:"editorLineNumbers,omitempty"` // 0 on, 1 relative, 2 off
	SoftWrap             bool                        `json:"editorSoftWrap,omitempty"`
	PanelPct             float64                     `json:"panelPercent,omitempty"`     // the editors' share over the bottom panel
	TermFontSize         float64                     `json:"terminalFontSize,omitempty"` // 12 when 0
	TermCopyOnSelect     bool                        `json:"terminalCopyOnSelect,omitempty"`
	Workspaces           map[string]workspaceSession `json:"workspaceSessions,omitempty"`

	path     string // where it is saved; "" saves nowhere, as in tests
	onRecent func() // called when the recent folders change
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "See", "config.json")
}

// loadAppConfig reads the former settings only when the See settings do not
// exist. Subsequent saves go to the new path and leave the old file intact.
func loadAppConfig(path string) *config {
	if path == "" {
		return loadConfig(path)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		legacy := filepath.Join(filepath.Dir(filepath.Dir(path)), "NativeSourceReader", "config.json")
		c := loadConfig(legacy)
		c.path = path
		return c
	}
	return loadConfig(path)
}

// loadConfig reads the configuration at p; a missing or broken file gives
// the defaults.
func loadConfig(p string) *config {
	c := &config{path: p}
	if p == "" {
		return c
	}
	if data, err := os.ReadFile(p); err == nil {
		if json.Unmarshal(data, c) != nil {
			*c = config{}
		}
		c.path = p
	}
	return c
}

// save writes the configuration, through a temporary file so a crash
// never leaves half of it.
func (c *config) save() error {
	if c.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *config) trusted(root string) bool { return slices.Contains(c.Trusted, root) }

func (c *config) trust(root string) {
	if !c.trusted(root) {
		c.Trusted = append(c.Trusted, root)
	}
}

// maxRecent is how many folders the welcome window lists.
const maxRecent = 12

// addRecent puts root first among the recent folders.
func (c *config) addRecent(root string) {
	c.removeRecent(root)
	c.Recent = slices.Insert(c.Recent, 0, root)
	if len(c.Recent) > maxRecent {
		c.Recent = c.Recent[:maxRecent]
	}
	c.recentChanged()
}

func (c *config) removeRecent(root string) {
	c.Recent = slices.DeleteFunc(c.Recent, func(p string) bool { return p == root })
	c.recentChanged()
}

func (c *config) clearRecent() {
	c.Recent = nil
	c.recentChanged()
	if err := c.save(); err != nil {
		log.Printf("saving settings: %v", err)
	}
}

func (c *config) recentChanged() {
	if c.onRecent != nil {
		c.onRecent()
	}
}

// fontSize is the editor's font size, 13 by default.
func (c *config) fontSize() float64 {
	if c.FontSize < 9 || c.FontSize > 28 {
		return 13
	}
	return c.FontSize
}

// panelPct is the editors' share of the height over the bottom panel.
func (c *config) panelPct() float64 {
	if c.PanelPct < 15 || c.PanelPct > 90 {
		return 66
	}
	return c.PanelPct
}

// termFontSize is the terminal's font size, 12 by default.
func (c *config) termFontSize() float64 {
	if c.TermFontSize < 9 || c.TermFontSize > 28 {
		return 12
	}
	return c.TermFontSize
}
