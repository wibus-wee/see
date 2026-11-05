package config

// Workspace stores only navigation and presentation state; file contents
// remain on disk and are loaded through the normal document pipeline.
type Workspace struct {
	Layout        *Layout `json:"layout,omitempty"`
	SidebarHidden bool    `json:"sidebarHidden,omitempty"`
	Navigator     int     `json:"navigator,omitempty"`
	// ChangesClosed and HistoryClosed are the Git navigator's sections
	// folded away.
	ChangesClosed bool    `json:"changesClosed,omitempty"`
	HistoryClosed bool    `json:"historyClosed,omitempty"`
	GitSplit      float32 `json:"gitSplit,omitempty"`    // the changes' share of the Git navigator, as a percentage
	GitLogSplit   float32 `json:"gitLogSplit,omitempty"` // the commits' share of the Git log's tab, as a percentage
	WindowWidth   int     `json:"windowWidth,omitempty"`
	WindowHeight  int     `json:"windowHeight,omitempty"`
}

// WorkspaceSize is the window size a workspace remembers, or the default
// when it remembers none, or nonsense.
func WorkspaceSize(s Workspace) (int, int) {
	if s.WindowWidth < 480 || s.WindowHeight < 320 || s.WindowWidth > 10000 || s.WindowHeight > 10000 {
		return DefaultWindowWidth, DefaultWindowHeight
	}
	return s.WindowWidth, s.WindowHeight
}

// DefaultWindowWidth and DefaultWindowHeight are the window size of a
// workspace that remembers none.
const DefaultWindowWidth, DefaultWindowHeight = 1280, 820

type Layout struct {
	Tabs []Tab `json:"tabs,omitempty"`
	// Active is the active tab's path, or a Git tab's key in the app.
	Active        string  `json:"active,omitempty"`
	ActivePreview bool    `json:"activePreview,omitempty"`
	Preview       string  `json:"preview,omitempty"`
	Focused       bool    `json:"focused,omitempty"`
	Vertical      bool    `json:"vertical,omitempty"`
	Percent       float32 `json:"percent,omitempty"`
	First         *Layout `json:"first,omitempty"`
	Second        *Layout `json:"second,omitempty"`
}

type Tab struct {
	Path            string `json:"path"`
	Untitled        bool   `json:"untitled,omitempty"`
	Name            string `json:"name,omitempty"`
	Line            int    `json:"line,omitempty"`
	Column          int    `json:"column,omitempty"`
	MarkdownPreview bool   `json:"markdownPreview,omitempty"`
	PreviewTab      bool   `json:"previewTab,omitempty"`
	// Diff is set for a tab of Git's, which Path is not a file of: a
	// change's diff, a commit, or the diff of a file of one.
	Diff *DiffTab `json:"diff,omitempty"`
}

// DiffTab is a tab of Git's, read again from the repository when the
// workspace opens: a change of the working tree or the index (Path,
// Staged, Untracked), a commit (Commit), or a file of one (Commit and
// Path, OldPath when renamed). Paths are the repository's, with slashes.
type DiffTab struct {
	Path      string `json:"path,omitempty"`
	OldPath   string `json:"oldPath,omitempty"`
	Staged    bool   `json:"staged,omitempty"`
	Untracked bool   `json:"untracked,omitempty"`
	Commit    string `json:"commit,omitempty"`
}
