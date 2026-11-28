package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"see/internal/atomicfile"
)

// writeBackupFile publishes the backup copy of a corrupted original.
// It is a variable so a test can fail it deterministically: the gate's
// scenario is a directory that cannot be written right now and can be
// written later, which no file system shape expresses on every
// platform — Windows has no mode bits that deny creating a file in a
// directory, and POSIX mode bits do not bind root.
var writeBackupFile = atomicfile.WriteFilePerm

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
	DiffUnified          bool                 `json:"diffUnified,omitempty"`       // diffs inline rather than side by side
	DiffLayout           string               `json:"diffLayout,omitempty"`        // "auto", "unified", "split"
	DiffShowUnchanged    bool                 `json:"diffShowUnchanged,omitempty"` // unchanged lines shown rather than folded
	GitStatus            bool                 `json:"gitStatus,omitempty"`
	GitAutoDiscover      bool                 `json:"gitAutoDiscover,omitempty"`
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

	// Beta contains unfinished features by id, each with its own settings
	// namespace. Registered migrations graduate or retire these entries.
	Beta map[string]BetaFeature `json:"betaFeatures,omitempty"`

	// LSP lists language servers the user configured, started for files
	// a built-in server does not cover. Same role as the servers See
	// wires in code, given by hand in the configuration file.
	LSP []LSPConfig `json:"lsp,omitempty"`

	path     string // where it is saved; "" saves nowhere, as in tests
	onRecent func() // called when the recent folders change

	// extras holds what the in-memory state cannot represent: keys this
	// build does not know, and values that do not fit their key's type.
	// Save writes them back unchanged, so a downgrade or an upgrade never
	// silently discards what the file said — with one exception: hiddenNames
	// is the struct's only non-omitempty field, so it always emits and wins
	// its collision, and a wrongly typed hiddenNames converges to the
	// default instead of surviving. Values that fit their type
	// but fail validation are not preserved — they are representable, and
	// the in-memory truth is authoritative for them; see MarshalJSON.
	extras map[string]json.RawMessage
	issues []Issue
	// stableKeys records every key the file declared, for beta-feature
	// graduation: a stable setting the user wrote explicitly outranks the
	// beta value it would graduate from.
	stableKeys map[string]bool
	// fileVersion is the schema version the file declared when it is
	// newer than this build's: Save writes it back rather than silently
	// downgrading the declaration of a file this build only partly
	// understands.
	fileVersion int
	// unprotected is the corrupted original file's bytes while no backup
	// of them exists anywhere. Load sets it when the parse fails and the
	// backup write fails with it; Save must complete the backup before
	// overwriting the only copy, as REQ-CFG-040's overwrite gate demands.
	unprotected []byte
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

// CurrentVersion is the configuration schema version this build writes.
// Version 0 is the unversioned schema of builds before the field existed;
// files without a "version" key are read as it. Version 1 adds nothing
// but the contract: the versions differ only in what readers may assume.
// No breaking change has ever needed a migration, so none exists; the
// contract for a breaking change is that it raises CurrentVersion, writes
// a backup before touching the file, and only then rewrites it.
const CurrentVersion = 1

// Load reads the configuration at p. It never fails: a missing file is
// the first run, and every problem degrades exactly the part of the file
// it affects — a broken syntax falls back to defaults, a mistyped key to
// that key's default — and is reported through Issues. Keys and values
// the load cannot use survive in memory and are written back on Save.
func Load(p string) *Config {
	c := &Config{path: p, HiddenNames: ".DS_Store"}
	if p == "" {
		return c
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			c.issues = append(c.issues, Issue{Kind: IssueReadFailed, Detail: err.Error()})
		}
		return c
	}
	betaChanged := c.decode(data)
	// Beta configuration converges on the current lifecycle registry —
	// active entries schema-pruned, graduated ones moved into stable
	// settings, retired ones dropped — and the rewrite reaches the file at
	// load time, so the migration is not contingent on the user ever
	// opening settings. The save honors the same overwrite gate as every
	// other; a failure logs and leaves the file for the next launch.
	if migrated := c.migrateBeta(); migrated || betaChanged {
		if err := c.Save(); err != nil {
			log.Printf("saving migrated settings: %v", err)
		}
	}
	return c
}

// decode fills the configuration from the file's bytes. The boolean
// reports beta configuration that should be rewritten in its clean shape:
// malformed entries pruned, the null namespace normalized away.
func (c *Config) decode(data []byte) (betaChanged bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		issue := Issue{Kind: IssueParseFailed, Detail: "the file is not a JSON object"}
		if err != nil {
			issue.Detail = err.Error()
		}
		// The original file stays exactly as it is; a backup copy
		// preserves it before anything can write the configuration back
		// over it. When the copy fails the original is the only version
		// left, and overwriting it is forbidden until the backup succeeds:
		// the bytes stay in memory as the gate Save enforces.
		backup := c.path + ".broken-" + time.Now().Format("20060102-150405.000000000")
		if err := writeBackupFile(backup, 0o600, func(w io.Writer) error {
			_, err := w.Write(data)
			return err
		}); err == nil {
			issue.Backup = backup
		} else {
			c.unprotected = data
			issue.Detail += " — and no backup copy could be written, so the broken file is the only one left: saving is refused until a backup succeeds"
		}
		c.issues = append(c.issues, issue)
		return
	}

	c.stableKeys = make(map[string]bool, len(raw))
	for key := range raw {
		c.stableKeys[key] = true
	}

	// The beta namespace parses in its own lenient shape — malformed beta
	// data degrades inside the namespace rather than invalidating the
	// stable keys around it, and entries no current feature registered
	// keep their bytes verbatim for the build they belong to. A value that
	// is not an object at all is representable by no beta shape: it is
	// preserved verbatim like any schema-invalid key.
	if betaRaw, hasBeta := raw["betaFeatures"]; hasBeta {
		delete(raw, "betaFeatures")
		if string(betaRaw) != "null" {
			var probe map[string]json.RawMessage
			if err := json.Unmarshal(betaRaw, &probe); err == nil && probe != nil {
				c.Beta, betaChanged = parseBetaFeatures(betaRaw)
			} else {
				c.issues = append(c.issues, Issue{Kind: IssueSchemaInvalid, Key: "betaFeatures", Detail: "the beta namespace is not an object"})
				c.preserve("betaFeatures", betaRaw)
			}
		} else {
			betaChanged = true
		}
	}
	// The pre-beta vibrancy key adopts into its beta feature: the key
	// never shipped as stable configuration, so nothing the file says is
	// lost, but the namespace it means moved.
	legacyVibrancy, hasLegacyVibrancy := raw["vibrancy"]
	legacyVibrancyKey := "vibrancy"
	if !hasLegacyVibrancy {
		legacyVibrancy, hasLegacyVibrancy = raw["Vibrancy"]
		legacyVibrancyKey = "Vibrancy"
	}
	if adoptVibrancy := hasLegacyVibrancy && betaFeatures[BetaSidebarVibrancy].active(); adoptVibrancy {
		delete(raw, legacyVibrancyKey)
		betaChanged = true
		var enabled bool
		if err := json.Unmarshal(legacyVibrancy, &enabled); err == nil && enabled {
			if _, exists := c.Beta[BetaSidebarVibrancy]; !exists {
				if c.Beta == nil {
					c.Beta = map[string]BetaFeature{}
				}
				c.Beta[BetaSidebarVibrancy] = BetaFeature{Enabled: true}
			}
		}
	}

	if v, ok := raw["version"]; ok {
		delete(raw, "version")
		var version int
		if err := json.Unmarshal(v, &version); err != nil {
			c.issues = append(c.issues, Issue{Kind: IssueSchemaInvalid, Key: "version", Detail: err.Error()})
			c.preserve("version", v)
		} else if version > CurrentVersion {
			c.issues = append(c.issues, Issue{
				Kind:   IssueVersionNewer,
				Detail: fmt.Sprintf("version %d is newer than this build's %d", version, CurrentVersion),
			})
			// The file stays the newer schema's: a save writes its version
			// back rather than silently downgrading the declaration.
			c.fileVersion = version
		}
	}

	type field struct {
		key   string
		value reflect.Value
	}
	fields := []field{}
	v := reflect.ValueOf(c).Elem()
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key == "" || key == "-" {
			continue
		}
		fields = append(fields, field{key, v.Field(i)})
	}
	for _, f := range fields {
		r, ok := raw[f.key]
		if !ok {
			continue
		}
		delete(raw, f.key)
		if err := json.Unmarshal(r, f.value.Addr().Interface()); err != nil {
			c.issues = append(c.issues, Issue{Kind: IssueSchemaInvalid, Key: f.key, Detail: err.Error()})
			c.preserve(f.key, r)
			continue
		}
		if detail := validate(f.key, f.value); detail != "" {
			f.value.SetZero()
			c.issues = append(c.issues, Issue{Kind: IssueValidationFailed, Key: f.key, Detail: detail})
			c.preserve(f.key, r)
		}
	}
	for key, r := range raw {
		c.preserve(key, r)
	}
	return betaChanged
}

// preserve keeps a key's original value for write-back.
func (c *Config) preserve(key string, r json.RawMessage) {
	if c.extras == nil {
		c.extras = map[string]json.RawMessage{}
	}
	c.extras[key] = r
}

// validate is the contractual domain of the keys that have one. Keys not
// checked here — scroll sensitivities, font weights, free-form strings —
// are read only through consumers that clamp or ignore what they do not
// understand, which is the degradation their domain defines.
func validate(key string, v reflect.Value) string {
	in := func(values ...string) string {
		if s := v.String(); s != "" && !slices.Contains(values, s) {
			return fmt.Sprintf("%q is not one of %s", s, strings.Join(values, ", "))
		}
		return ""
	}
	inRange := func(lo, hi float64) string {
		if n := v.Float(); n != 0 && (n < lo || n > hi) {
			return fmt.Sprintf("%v is outside %v..%v", n, lo, hi)
		}
		return ""
	}
	inIntRange := func(lo, hi int64) string {
		if n := v.Int(); n != 0 && (n < lo || n > hi) {
			return fmt.Sprintf("%d is outside %d..%d", n, lo, hi)
		}
		return ""
	}
	switch key {
	case "appearance":
		return in("light", "dark")
	case "inspectorTab":
		return in("quickhelp", "file", "history")
	case "terminalCursor":
		return in("block", "bar", "underline")
	case "terminalRightClick":
		return in("menu", "paste", "copyOrPaste")
	case "terminalClipboard":
		return in("ask", "allow", "deny")
	case "parameterHelp":
		return in("trigger", "always", "off")
	case "diffLayout":
		return in("auto", "unified", "split")
	case "editorFontSize":
		return inRange(9, 28)
	case "terminalFontSize":
		return inRange(9, 28)
	case "editorLineNumbers":
		return inIntRange(0, 2)
	case "editorPageGuideColumn":
		return inIntRange(1, MaxPageGuideColumn)
	case "panelPercent":
		return inRange(15, 90)
	case "terminalLineHeight":
		return inRange(-20, 100)
	}
	return ""
}

// MarshalJSON writes the struct's fields, the schema version, and the
// preserved keys the struct does not know. The write-back contract decides
// per key what the file says afterwards:
//
//   - Keys the struct does not know, and values that do not fit their
//     key's type, are preserved verbatim: the in-memory state cannot
//     represent them, so nothing in memory may overrule what the file
//     said. A struct field that validates again still wins its collision,
//     because then the typed value is the configuration.
//   - Values that fit their type but failed validation are not preserved.
//     The in-memory state — the default the load degraded to, or whatever
//     the user chose since — is the semantic truth the settings display
//     and the app runs on, and the file converges to it. Without this rule
//     a repair to the default could never reach the file: the zero value
//     emits no key, so the preserved invalid value would win the collision
//     back, the file could never heal, and the warning would repeat on
//     every launch while the settings already showed the intended state.
//     The cost is the downgrade path of an enum widened by a newer build:
//     this build cannot validate the newer value either, so a save
//     replaces it with the in-memory state rather than keeping it for the
//     upgrade back. The file's content is the truth the user is running
//     on either way; nothing is silently destroyed, because the invalid
//     value was never in effect.
//
// An explicit null reads as the zero value of its key's type — unset,
// meaning the default — and write-back normalizes it to the same omission
// the zero value gets. The version key follows the same representability
// rule: a version this build cannot read as a number is preserved, because
// the schema integer is not what the file declared.
func (c *Config) MarshalJSON() ([]byte, error) {
	type exported Config
	data, err := json.Marshal((*exported)(c))
	if err != nil {
		return nil, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(data, &merged); err != nil {
		return nil, err
	}
	version := CurrentVersion
	if c.fileVersion > version {
		version = c.fileVersion
	}
	// A wrongly-shaped version declaration — the file says a string where
	// the schema reads a number — is not representable in memory, so the
	// file keeps it: writing the schema integer over it would silently
	// rewrite what the file claims to be.
	if _, foreign := c.extras["version"]; !foreign {
		if b, err := json.Marshal(version); err == nil {
			merged["version"] = b
		}
	}
	fields := fieldTypes()
	for key, r := range c.extras {
		if ft, known := fields[key]; known {
			probe := reflect.New(ft)
			if json.Unmarshal(r, probe.Interface()) == nil {
				// The value fits its key's type: it is here because
				// validation rejected it, and validation-failed keys are
				// representable — the in-memory truth is authoritative.
				// A non-zero field emits into merged and wins by absence
				// of competition; a zero field emits nothing, and omitting
				// the key is exactly what writing that state means.
				continue
			}
		}
		if _, taken := merged[key]; !taken {
			merged[key] = r
		}
	}
	return json.Marshal(merged)
}

// fieldTypes maps each configuration key to the type of its struct field.
func fieldTypes() map[string]reflect.Type {
	m := map[string]reflect.Type{}
	t := reflect.TypeOf(Config{})
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		key, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if key == "" || key == "-" {
			continue
		}
		m[key] = f.Type
	}
	return m
}

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

// Save writes the configuration through the atomic write contract: the
// complete new file is staged beside the old one, made durable, and
// published in one rename. A failure anywhere reports failure, leaves the
// old configuration intact, and cleans its staging up.
//
// One refusal overrides the convenience: when the file being overwritten
// is a corrupted original whose backup never completed, the original is
// the only copy of what the user had. The overwrite gate of the
// configuration contract allows replacing it only after the backup
// completes, so Save first writes the backup of the bytes Load kept, and
// refuses — changing nothing — when even that fails. The common transient
// case, a full disk or a permission problem at launch that has since been
// fixed, heals on the first save: the backup lands, the overwrite
// proceeds, and the file is never unprotected at any moment.
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
	if len(c.unprotected) > 0 {
		backup := c.path + ".broken-" + time.Now().Format("20060102-150405.000000000")
		if err := writeBackupFile(backup, 0o600, func(w io.Writer) error {
			_, err := w.Write(c.unprotected)
			return err
		}); err != nil {
			return fmt.Errorf("saving is refused: the configuration file is corrupted and backing it up failed again (%v) — the broken file is the only copy of the old settings, so nothing was overwritten", err)
		}
		// The original is protected now wherever it goes next: overwrite
		// away, and let the load issue know where the copy landed.
		c.unprotected = nil
		for i := range c.issues {
			if c.issues[i].Kind == IssueParseFailed && c.issues[i].Backup == "" {
				c.issues[i].Backup = backup
			}
		}
	}
	return atomicfile.WriteFilePerm(c.path, 0o600, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
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
