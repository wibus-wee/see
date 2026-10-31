package config

// Workspace stores only navigation and presentation state; file contents
// remain on disk and are loaded through the normal document pipeline.
type Workspace struct {
	Layout        *Layout `json:"layout,omitempty"`
	SidebarHidden bool    `json:"sidebarHidden,omitempty"`
	Navigator     int     `json:"navigator,omitempty"`
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
	Tabs          []Tab   `json:"tabs,omitempty"`
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
}
