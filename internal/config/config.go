package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
)

// Config is what the app remembers between runs.
type Config struct {
	LastWorkspace        string               `json:"lastWorkspace,omitempty"`
	Recent               []string             `json:"recentWorkspaces,omitempty"`    // newest first
	ReopenLast           bool                 `json:"reopenLastWorkspace,omitempty"` // else the welcome window shows at launch
	SidebarWidth         float32              `json:"sidebarWidth,omitempty"`
	Appearance           string               `json:"appearance,omitempty"` // "light", "dark", or "" for the system's
	Trusted              []string             `json:"trustedWorkspaces,omitempty"`
	FontFamily           string               `json:"editorFontFamily,omitempty"`
	TermFontFallback     string               `json:"terminalFontFallback,omitempty"`
	TermFontFamily       string               `json:"terminalFontFamily,omitempty"`
	ExternalTerminal     string               `json:"externalTerminal,omitempty"`
	ExternalTerminalPath string               `json:"externalTerminalPath,omitempty"`
	TermFontWeight       int                  `json:"terminalFontWeight,omitempty"`
	TermLineHeight       float64              `json:"terminalLineHeight,omitempty"`
	TermThicken          bool                 `json:"terminalFontSmoothing,omitempty"`
	TermNoLigatures      bool                 `json:"terminalNoLigatures,omitempty"`
	FontSize             float64              `json:"editorFontSize,omitempty"`    // 13 when 0
	LineNumbers          int                  `json:"editorLineNumbers,omitempty"` // 0 on, 1 relative, 2 off
	SoftWrap             bool                 `json:"editorSoftWrap,omitempty"`
	PanelPct             float64              `json:"panelPercent,omitempty"`     // the editors' share over the bottom panel
	TermFontSize         float64              `json:"terminalFontSize,omitempty"` // 12 when 0
	TermCopyOnSelect     bool                 `json:"terminalCopyOnSelect,omitempty"`
	Workspaces           map[string]Workspace `json:"workspaceSessions,omitempty"`

	path     string // where it is saved; "" saves nowhere, as in tests
	onRecent func() // called when the recent folders change
}

// Path is where the configuration lives.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "See", "config.json")
}

// Load reads the configuration at p; a missing or broken file gives the
// defaults.
func Load(p string) *Config {
	c := &Config{path: p}
	if p == "" {
		return c
	}
	if data, err := os.ReadFile(p); err == nil {
		if json.Unmarshal(data, c) != nil {
			*c = Config{}
		}
		c.path = p
	}
	return c
}

// save writes the configuration, through a temporary file so a crash
// never leaves half of it.
// SetPath moves the configuration to p, which the next Save writes.
func (c *Config) SetPath(p string) { c.path = p }

// Path is where this configuration is saved.
func (c *Config) Path() string { return c.path }

func (c *Config) Save() error {
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

// IsTrusted reports whether root is a workspace the user trusts.
func (c *Config) IsTrusted(root string) bool { return slices.Contains(c.Trusted, root) }

func (c *Config) Trust(root string) {
	if !c.IsTrusted(root) {
		c.Trusted = append(c.Trusted, root)
	}
}

// MaxRecent is how many folders the welcome window lists.
const MaxRecent = 12

// addRecent puts root first among the recent folders.
func (c *Config) AddRecent(root string) {
	c.RemoveRecent(root)
	c.Recent = slices.Insert(c.Recent, 0, root)
	if len(c.Recent) > MaxRecent {
		c.Recent = c.Recent[:MaxRecent]
	}
	c.recentChanged()
}

func (c *Config) RemoveRecent(root string) {
	c.Recent = slices.DeleteFunc(c.Recent, func(p string) bool { return p == root })
	c.recentChanged()
}

func (c *Config) ClearRecent() {
	c.Recent = nil
	c.recentChanged()
	if err := c.Save(); err != nil {
		log.Printf("saving settings: %v", err)
	}
}

// SetRecentHook sets what runs when the recent folders change.
func (c *Config) SetRecentHook(fn func()) { c.onRecent = fn }

func (c *Config) recentChanged() {
	if c.onRecent != nil {
		c.onRecent()
	}
}

// EditorFontSize is the editor's font size, 13 by default.
func (c *Config) EditorFontSize() float64 {
	if c.FontSize < 9 || c.FontSize > 28 {
		return 13
	}
	return c.FontSize
}

// EditorPanelPct is the editors' share of the height over the panel.
func (c *Config) EditorPanelPct() float64 {
	if c.PanelPct < 15 || c.PanelPct > 90 {
		return 66
	}
	return c.PanelPct
}

// TerminalFontSize is the terminal's font size, 12 by default.
func (c *Config) TerminalFontSize() float64 {
	if c.TermFontSize < 9 || c.TermFontSize > 28 {
		return 12
	}
	return c.TermFontSize
}
