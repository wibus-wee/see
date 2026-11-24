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
	HideInlineDiagnostics bool `json:"hideInlineDiagnostics,omitempty"`

	HideWhitespaceChanges bool `json:"hideWhitespaceChanges,omitempty"`

	LastWorkspace        string               `json:"lastWorkspace,omitempty"`
	Recent               []string             `json:"recentWorkspaces,omitempty"`    // newest first
	ReopenLast           bool                 `json:"reopenLastWorkspace,omitempty"` // else the welcome window shows at launch
	SidebarWidth         float32              `json:"sidebarWidth,omitempty"`
	QuickHelpInspector   bool                 `json:"quickHelpInspector,omitempty"` // the inspector beside the editors
	QuickHelpWidth       float32              `json:"quickHelpWidth,omitempty"`     // its width; 0 is the default
	InspectorTab         string               `json:"inspectorTab,omitempty"`       // its page: "quickhelp" (default), "file", or "history"
	Appearance           string               `json:"appearance,omitempty"`         // "light", "dark", or "" for the system's
	Language             string               `json:"language,omitempty"`           // "system", or a language tag
	Vibrancy             bool                 `json:"vibrancy,omitempty"`           // the window's material under the wash
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
	TermFontFeatures     []string             `json:"terminalFontFeatures,omitempty"`
	TermCursor           string               `json:"terminalCursor,omitempty"` // "block", "bar", "underline"
	TermCursorNoBlink    bool                 `json:"terminalCursorNoBlink,omitempty"`
	TermOptionAsAlt      bool                 `json:"terminalOptionAsAlt,omitempty"`
	TermRightClick       string               `json:"terminalRightClick,omitempty"` // "menu", "paste", "copyOrPaste"
	TermClipboard        string               `json:"terminalClipboard,omitempty"`  // "ask", "allow", "deny"
	TermScrollPrecision  float64              `json:"terminalScrollPrecision,omitempty"`
	TermScrollDiscrete   float64              `json:"terminalScrollDiscrete,omitempty"`
	TermMinContrast      float64              `json:"terminalMinContrast,omitempty"`
	TermDimUnfocused     bool                 `json:"terminalDimUnfocused,omitempty"`
	TermScrollback       int                  `json:"terminalScrollback,omitempty"`
	TermEnv              []string             `json:"terminalEnv,omitempty"`
	TermShell            string               `json:"terminalShell,omitempty"`
	EditorFontWeight     int                  `json:"editorFontWeight,omitempty"`
	EditorLineHeight     float64              `json:"editorLineHeight,omitempty"`
	EditorNoLigatures    bool                 `json:"editorNoLigatures,omitempty"`
	EditorFontFeatures   []string             `json:"editorFontFeatures,omitempty"`
	EditorThicken        bool                 `json:"editorFontThicken,omitempty"`
	FontSize             float64              `json:"editorFontSize,omitempty"`    // 13 when 0
	LineNumbers          int                  `json:"editorLineNumbers,omitempty"` // 0 on, 1 relative, 2 off
	SoftWrap             bool                 `json:"editorSoftWrap,omitempty"`
	SoftWrapByLanguage   map[string]bool      `json:"editorSoftWrapByLanguage,omitempty"`
	EditorShowPageGuide  bool                 `json:"editorShowPageGuide,omitempty"`
	EditorPageGuide      int                  `json:"editorPageGuideColumn,omitempty"`
	HideDocComments      bool                 `json:"hideDocComments,omitempty"`
	ShowInvisibles       bool                 `json:"editorShowInvisibles,omitempty"`
	EditorFoldImports    bool                 `json:"editorFoldImports,omitempty"`
	ParameterHelp        string               `json:"parameterHelp,omitempty"` // "trigger" (default), "always", "off"
	HideInlineBlame      bool                 `json:"hideInlineGitBlame,omitempty"`
	HideTypeHints        bool                 `json:"hideTypeHints,omitempty"`
	HideSymbolHighlights bool                 `json:"hideSymbolHighlights,omitempty"`
	HideMinimap          bool                 `json:"hideEditorMinimap,omitempty"`
	HideReadingPin       bool                 `json:"hideReadingPositionPin,omitempty"`
	DiffUnified          bool                 `json:"diffUnified,omitempty"` // diffs inline rather than side by side
	DiffLayout           string               `json:"diffLayout,omitempty"`  // "auto", "unified", "split"
	GitStatus            bool                 `json:"gitStatus,omitempty"`
	HiddenNames          string               `json:"hiddenNames"`
	ConfirmClose         bool                 `json:"confirmClose,omitempty"`
	KeepAwake            string               `json:"keepAwake,omitempty"`
	AppIcon              string               `json:"appIcon,omitempty"`
	NotifyWaiting        bool                 `json:"notifyWaiting,omitempty"`
	NotifyDone           bool                 `json:"notifyDone,omitempty"`
	NotifyFailed         bool                 `json:"notifyFailed,omitempty"`
	NotifySound          bool                 `json:"notifySound,omitempty"`
	DockBadge            bool                 `json:"dockBadge,omitempty"`
	BounceDock           bool                 `json:"bounceDock,omitempty"`
	RemoteControl        bool                 `json:"remoteControl,omitempty"`
	Shortcuts            map[string]string    `json:"shortcuts,omitempty"`
	PanelPct             float64              `json:"panelPercent,omitempty"`     // the editors' share over the bottom panel
	TermFontSize         float64              `json:"terminalFontSize,omitempty"` // 12 when 0
	TermCopyOnSelect     bool                 `json:"terminalCopyOnSelect,omitempty"`
	Workspaces           map[string]Workspace `json:"workspaceSessions,omitempty"`

	SavedFindQueries map[string][]SavedFindQuery `json:"savedFindQueries,omitempty"`

	// LSP lists language servers the user configured, started for files
	// a built-in server does not cover. Same role as the servers See
	// wires in code, given by hand in the configuration file.
	LSP []LSPConfig `json:"lsp,omitempty"`

	path     string // where it is saved; "" saves nowhere, as in tests
	onRecent func() // called when the recent folders change
}

type SavedFindQuery struct {
	Query         string `json:"query"`
	CaseSensitive bool   `json:"caseSensitive,omitempty"`
	WholeWord     bool   `json:"wholeWord,omitempty"`
	Regexp        bool   `json:"regexp,omitempty"`
	Include       string `json:"include,omitempty"`
	Exclude       string `json:"exclude,omitempty"`
}

// LSPConfig describes one configured language server: what it is called,
// the files it covers, and the command that starts it.
//
// In the configuration file, under the "lsp" array:
//
//	"lsp": [
//	  {
//	    "name": "rust-analyzer",
//	    "extensions": [".rs"],
//	    "languages": ["rust"],
//	    "command": ["rust-analyzer"],
//	    "env": {"RA_LOG": "info"},
//	    "initializationOptions": {"check": {"command": "clippy"}}
//	  }
//	]
type LSPConfig struct {
	// Name is what errors, logs and Settings call the server, such as
	// "rust-analyzer". Required.
	Name string `json:"name"`
	// Extensions are the file extensions the server covers, with their
	// leading dot, such as [".rs", ".py"]. A file whose extension is
	// listed is handled by this server when no built-in server claims it.
	Extensions []string `json:"extensions,omitempty"`
	// Languages are the LSP language IDs the server covers, such as
	// ["rust"]. The first entry is also the language ID sent in
	// didOpen for files matched by extension.
	Languages []string `json:"languages,omitempty"`
	// Command is the executable and its arguments, as exec accepts them:
	// ["rust-analyzer"] or ["pyright-langserver", "--stdio"]. The first
	// element is resolved on PATH; an absolute path is used as given.
	Command []string `json:"command"`
	// Env adds environment variables to the server's process, merged
	// over the environment See itself runs with.
	Env map[string]string `json:"env,omitempty"`
	// InitializationOptions is sent verbatim as the LSP
	// initializationOptions, for servers that take settings there.
	InitializationOptions any `json:"initializationOptions,omitempty"`
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
	c := &Config{path: p, HiddenNames: ".DS_Store"}
	if p == "" {
		return c
	}
	if data, err := os.ReadFile(p); err == nil {
		if json.Unmarshal(data, c) != nil {
			*c = Config{HiddenNames: ".DS_Store"}
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

// SoftWrapForLanguage returns the language override or the global default.
func (c *Config) SoftWrapForLanguage(language string) bool {
	if wrap, ok := c.SoftWrapByLanguage[language]; ok {
		return wrap
	}
	return c.SoftWrap
}

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

const MaxPageGuideColumn = 500

func (c *Config) PageGuideColumn() int {
	if c.EditorPageGuide < 1 || c.EditorPageGuide > MaxPageGuideColumn {
		return 80
	}
	return c.EditorPageGuide
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
