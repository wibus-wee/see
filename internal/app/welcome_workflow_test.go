package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/internal/config"
	"see/internal/workspace"
)

func TestWelcomeActionsAndEmptyState(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	h.tt.Frame()

	for _, text := range []string{
		"New File", "Start with a blank text file", shortcutLabel("N"),
		"Open File", "Open Folder", "No recent folders", "Folders you open will appear here.",
	} {
		if !h.tt.HasText(text) {
			t.Errorf("welcome is missing %q", text)
		}
	}
	if !h.tt.Focused("New File") {
		t.Error("the first welcome action is not keyboard-focused")
	}
	texts := h.tt.Texts()
	position := func(want string) int {
		for i, text := range texts {
			if text == want {
				return i
			}
		}
		return -1
	}
	newAt, fileAt, folderAt := position("New File"), position("Open File"), position("Open Folder")
	if newAt < 0 || fileAt <= newAt || folderAt <= fileAt {
		t.Fatalf("welcome action order is New=%d File=%d Folder=%d in %v", newAt, fileAt, folderAt, texts)
	}
}

func TestWelcomeReopenStaysAboveLaunchCheckbox(t *testing.T) {
	h := newHarness(t, "")
	h.a.version = "0.3.0"
	h.a.cfg.Recent = []string{"/previous/project", "/older/project"}
	tt := ui.NewTester(h.a.welcomeView, welcomeWidth, welcomeHeight)
	tt.Frame()

	reopen, ok := tt.Find(welcomeText("welcomeReopenRecent"))
	if !ok {
		t.Fatal("reopen action is missing")
	}
	checkbox, ok := tt.Find("Show this window when See launches")
	if !ok {
		t.Fatal("launch checkbox is missing")
	}
	if reopen.Y+reopen.H > checkbox.Y {
		t.Fatalf("reopen action (bottom %v) overlaps the launch checkbox (top %v)", reopen.Y+reopen.H, checkbox.Y)
	}
	if checkbox.Y+checkbox.H > float32(welcomeHeight) {
		t.Fatalf("launch checkbox (bottom %v) runs past the window's height %v", checkbox.Y+checkbox.H, welcomeHeight)
	}
}

func TestNewFileShortcutFromFolderlessWelcome(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	resized := 0
	h.host.resize = func(width, height int) { resized++ }

	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyN)
	h.tt.Frame()
	h.tt.Frame() // a held-over frame must not dispatch the shortcut again

	if len(h.a.docs) != 1 {
		t.Fatalf("Cmd+N created %d drafts, want one", len(h.a.docs))
	}
	d := h.a.docs[h.a.activePath()]
	if d == nil || !d.untitled || d.name != "Untitled" || h.a.showWelcome {
		t.Fatalf("unexpected draft/welcome state: draft=%+v showWelcome=%v", d, h.a.showWelcome)
	}
	if h.a.activeEditor() == nil {
		t.Fatal("new draft did not open an editor")
	}
	if !h.tt.Focused("Untitled") {
		t.Fatal("new draft editor is not keyboard-focused")
	}
	if resized != 1 {
		t.Fatalf("folderless welcome resized to a workspace %d times, want one", resized)
	}
}

func TestNewFileShortcutFromStandaloneWelcome(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	tt := ui.NewTester(h.a.welcomeView, welcomeWidth, welcomeHeight)

	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()

	if len(h.a.docs) != 1 || h.a.showWelcome {
		t.Fatalf("standalone welcome Cmd+N: docs=%d showWelcome=%v", len(h.a.docs), h.a.showWelcome)
	}
}

func TestNewFileFromWorkspaceWelcomeOverlay(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"keep.txt": "keep"}))
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.Key1)
	h.tt.Frame()
	if !h.a.showWelcome {
		t.Fatal("Shift+Cmd+1 did not show the welcome overlay")
	}

	tt := ui.NewTester(h.a.welcomeView, welcomeWidth, welcomeHeight)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyN)
	tt.Frame()

	if len(h.a.docs) != 1 || h.a.showWelcome {
		t.Fatalf("overlay Cmd+N: docs=%d showWelcome=%v", len(h.a.docs), h.a.showWelcome)
	}
	if d := h.a.docs[h.a.activePath()]; d == nil || !d.untitled {
		t.Fatalf("overlay did not open an untitled draft: %+v", d)
	}
}

func TestFileMenuNewFileEnabledOnWelcome(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	menu := h.a.fileMenu(func(fn func()) { fn() })
	item := menu.Submenu[0]
	if item.Disabled {
		t.Fatal("New File menu item is disabled on welcome")
	}
	if item.Accelerator != "" {
		t.Fatalf("New File menu accelerator = %q, want shortcut handled by the active view", item.Accelerator)
	}

	item.Click(nil, nil)
	if len(h.a.docs) != 1 || h.a.showWelcome {
		t.Fatalf("New File menu action: docs=%d showWelcome=%v", len(h.a.docs), h.a.showWelcome)
	}
}

func TestNewWindowShortcutStillDoesNotCreateDraft(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	opened := 0
	h.host.openProject = func(path string) bool {
		if path != "" {
			t.Fatalf("new window path = %q", path)
		}
		opened++
		return true
	}
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyN)
	h.tt.Frame()

	if opened != 1 || len(h.a.docs) != 0 {
		t.Fatalf("Shift+Cmd+N opened=%d drafts=%d", opened, len(h.a.docs))
	}
}

func TestUntitledSaveCancelPreservesDraft(t *testing.T) {
	h := newHarness(t, "")
	h.a.newFile()
	d := h.a.docs[h.a.activePath()]
	d.buf.Set("keep this text")
	oldPath := d.path
	panels := 0
	h.host.newFile = func(string, string) (string, error) {
		panels++
		return "", nil
	}

	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyS)
	h.tt.Frame()
	h.settle(func() bool { return !d.saving })

	if panels != 1 || d.path != oldPath || !d.untitled || d.buf.String() != "keep this text" || !d.buf.Modified() {
		t.Fatalf("cancel changed draft: panels=%d path=%q untitled=%v text=%q dirty=%v", panels, d.path, d.untitled, d.buf.String(), d.buf.Modified())
	}
	if h.a.activePath() != oldPath || h.a.activeEditor() == nil {
		t.Fatal("cancel changed the active editor")
	}
	if !h.tt.Focused("Untitled") {
		t.Fatal("cancel did not return keyboard focus to the untitled editor")
	}
}

func TestFolderlessDraftRestoresFromSavedSession(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Load(configPath)
	cfg.Workspaces = map[string]config.Workspace{
		"/existing-project": {SidebarHidden: true},
	}
	h := newHarness(t, "")
	h.a.cfg = cfg
	h.a.newFile()
	draft := h.a.docs[h.a.activePath()]
	draft.buf.Set("recover after restart")
	h.tt.Frame()
	h.a.saveConfig()
	h.settle(func() bool {
		data, err := os.ReadFile(draft.path)
		return err == nil && string(data) == "recover after restart"
	})

	reloaded := config.Load(configPath)
	if !reloaded.Workspaces["/existing-project"].SidebarHidden {
		t.Fatal("saving the standalone session changed an existing workspace session")
	}
	next := newHarness(t, "")
	next.a.cfg = reloaded
	next.a.restoreSession()
	next.settle(func() bool {
		d := next.a.docs[draft.path]
		return d != nil && !d.loading
	})
	restored := next.a.docs[draft.path]
	if restored == nil {
		t.Fatal("standalone session did not restore the draft")
	}
	if restored.name != "Untitled" || restored.buf == nil || restored.buf.String() != "recover after restart" || !restored.buf.Modified() {
		text := "<nil>"
		if restored.buf != nil {
			text = restored.buf.String()
		}
		t.Fatalf("restored draft = %+v, text=%q", restored, text)
	}

	// Additional folderless windows share Config, but only the primary owns
	// and restores the empty-root session.
	child := newApp(reloaded)
	child.ownsStandaloneSession = false
	child.restoreSession()
	if len(child.docs) != 0 {
		t.Fatalf("secondary folderless window duplicated %d restored documents", len(child.docs))
	}
	child.saveConfig()
	if reloaded.Workspaces[""].Layout == nil {
		t.Fatal("secondary folderless window clobbered the primary standalone session")
	}
}

func TestStandaloneSessionDoesNotReopenSavedFiles(t *testing.T) {
	h := newHarness(t, "")
	h.a.newFile()
	draft := h.a.docs[h.a.activePath()]
	draft.buf.Set("saved text")
	destination := filepath.Join(t.TempDir(), "saved.txt")
	h.host.newFile = func(string, string) (string, error) { return destination, nil }

	h.a.saveActive()
	h.settle(func() bool { return !draft.saving && !draft.untitled })

	next := newHarness(t, "")
	next.a.cfg = h.a.cfg
	next.a.restoreSession()
	if len(next.a.docs) != 0 {
		t.Fatalf("standalone session reopened %d saved files", len(next.a.docs))
	}
}

func standaloneSessionHas(layout *config.Layout, path string) bool {
	if layout == nil {
		return false
	}
	for _, tab := range layout.Tabs {
		if tab.Path == path {
			return true
		}
	}
	return standaloneSessionHas(layout.First, path) || standaloneSessionHas(layout.Second, path)
}

func TestStandaloneDirtySavedFileRestoresFromRecovery(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	path := filepath.Join(t.TempDir(), "saved.txt")
	if err := os.WriteFile(path, []byte("on disk"), 0600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "")
	h.a.cfg = config.Load(configPath)
	d := h.a.openFile(path, nil)
	h.settle(func() bool { return !d.loading })
	d.buf.Set("dirty recovery")
	h.a.cacheRecovery(d)
	h.settle(func() bool {
		r := readRecovery("", path)
		return r != nil && r.Text == "dirty recovery"
	})
	h.a.saveConfig()
	// The next app simulates a new process after a crash; process-local path
	// ownership disappears even though the recovery files and config remain.
	releaseStandalonePaths(h.a)

	reloaded := config.Load(configPath)
	if !standaloneSessionHas(reloaded.Workspaces[""].Layout, path) {
		t.Fatal("dirty saved file is not reachable from the standalone session")
	}
	next := newHarness(t, "")
	next.a.cfg = reloaded
	next.a.restoreSession()
	next.settle(func() bool { return next.a.docs[path] != nil && !next.a.docs[path].loading })
	restored := next.a.docs[path]
	if restored.buf.String() != "dirty recovery" || !restored.buf.Modified() {
		t.Fatalf("restored saved file = %q dirty=%v", restored.buf.String(), restored.buf.Modified())
	}

	if !next.a.saveDoc(restored, false) {
		t.Fatal("saving the restored file failed")
	}
	afterSave := config.Load(configPath)
	if standaloneSessionHas(afterSave.Workspaces[""].Layout, path) {
		t.Fatal("clean saved file remained in the standalone session")
	}
}

func TestSecondaryStandaloneDraftsMergeAndRestoreInPrimary(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	shared := config.Load(configPath)
	makeDraft := func(owner bool, text string) (*harness, *doc) {
		h := newHarness(t, "")
		h.a.cfg = shared
		h.a.ownsStandaloneSession = owner
		h.a.newFile()
		d := h.a.docs[h.a.activePath()]
		d.buf.Set(text)
		h.a.cacheDraft(d)
		h.settle(func() bool {
			data, err := os.ReadFile(d.path)
			return err == nil && string(data) == text
		})
		h.a.saveConfig()
		return h, d
	}
	primary, first := makeDraft(true, "primary draft")
	child, second := makeDraft(false, "secondary draft")
	_, third := makeDraft(false, "other secondary draft")

	// A later save by either window must retain all other windows' entries.
	primary.a.saveConfig()
	child.a.saveConfig()
	for _, path := range []string{first.path, second.path, third.path} {
		if !standaloneSessionHas(shared.Workspaces[""].Layout, path) {
			t.Fatalf("merged standalone session lost %s", path)
		}
	}
	child.a.restoreSession()
	if len(child.a.docs) != 1 || child.a.docs[second.path] == nil {
		t.Fatalf("secondary restored the shared session: docs=%v", child.a.docs)
	}

	next := newHarness(t, "")
	next.a.cfg = config.Load(configPath)
	next.a.restoreSession()
	next.settle(func() bool {
		if len(next.a.docs) != 3 {
			return false
		}
		for _, d := range next.a.docs {
			if d.loading {
				return false
			}
		}
		return true
	})
	for path, want := range map[string]string{first.path: "primary draft", second.path: "secondary draft", third.path: "other secondary draft"} {
		if d := next.a.docs[path]; d == nil || d.buf.String() != want || !d.buf.Modified() {
			t.Fatalf("recovered %s = %+v, want %q", path, d, want)
		}
	}
}

func TestDiscardBeforeWorkspaceTransitionRemovesStandaloneDraftSession(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	h := newHarness(t, "")
	h.a.cfg = config.Load(configPath)
	h.a.newFile()
	d := h.a.docs[h.a.activePath()]
	d.buf.Set("discard me")
	h.a.saveConfig()
	h.host.confirm = func(string) (int, error) { return 1, nil }
	root := writeTree(t, map[string]string{"keep.txt": "keep"})

	h.a.openWorkspace(root)
	h.settle(func() bool { return h.a.root == root })
	if standaloneSessionHas(config.Load(configPath).Workspaces[""].Layout, d.path) {
		t.Fatal("workspace transition left the discarded draft in the saved standalone session")
	}
}

func TestDiscardBeforeWorkspaceTransitionRemovesWorkspaceDraftSession(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	first := writeTree(t, map[string]string{"one.txt": "one"})
	h := newHarness(t, first)
	h.a.cfg.SetPath(configPath)
	h.a.newFile()
	d := h.a.docs[h.a.activePath()]
	d.buf.Set("discard workspace draft")
	h.a.saveConfig()
	h.host.confirm = func(string) (int, error) { return 1, nil }
	second := writeTree(t, map[string]string{"two.txt": "two"})

	h.a.openWorkspace(second)
	h.settle(func() bool { return h.a.root == second })
	if standaloneSessionHas(config.Load(configPath).Workspaces[first].Layout, d.path) {
		t.Fatal("workspace transition left the discarded draft in the old workspace session")
	}
}

func TestDiscardBeforeQuitStaysOutOfSynchronousSessionSave(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	h := newHarness(t, "")
	h.a.cfg = config.Load(configPath)
	h.a.cfg.ConfirmClose = true
	h.a.newFile()
	d := h.a.docs[h.a.activePath()]
	d.buf.Set("discard on quit")
	h.a.saveConfig()
	h.host.confirm = func(string) (int, error) { return 1, nil }
	proceeded := false

	if !h.a.guardDirty(h.a.dirtyDocs(), func() {
		proceeded = true
		// OnWillQuit saves every window once more.
		h.a.saveConfig()
	}) {
		t.Fatal("dirty quit was not guarded")
	}
	h.settle(func() bool { return proceeded })
	if standaloneSessionHas(config.Load(configPath).Workspaces[""].Layout, d.path) {
		t.Fatal("quit save put the discarded draft back in the standalone session")
	}
}

func TestStandaloneMatchingRecoveryDoesNotReopenSavedFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	path := filepath.Join(t.TempDir(), "saved.txt")
	if err := os.WriteFile(path, []byte("already saved"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := recoveryPath("", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(recoveryRecord{Path: path, Base: "before", Text: "already saved"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load(configPath)
	cfg.Workspaces = map[string]config.Workspace{"": {Layout: &config.Layout{Tabs: []config.Tab{{Path: path}}}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, "")
	h.a.cfg = config.Load(configPath)
	h.a.restoreSession()
	if len(h.a.docs) != 0 {
		t.Fatalf("matching recovery reopened %d saved files", len(h.a.docs))
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("matching recovery record remains: %v", err)
	}
	h.a.saveConfig()
	if standaloneSessionHas(config.Load(configPath).Workspaces[""].Layout, path) {
		t.Fatal("matching recovery left a stale standalone session entry")
	}
}

func TestStandalonePathClaimIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reserved.txt")
	first, second := newHarness(t, ""), newHarness(t, "")
	one := &doc{path: "first"}
	two := &doc{path: "second"}
	if !claimStandalonePath(first.a, one, path) {
		t.Fatal("first standalone save could not reserve its destination")
	}
	if claimStandalonePath(second.a, two, path) {
		t.Fatal("second standalone save reserved the same destination")
	}
	releaseStandalonePath(first.a, one, path)
	if !claimStandalonePath(second.a, two, path) {
		t.Fatal("destination did not become available after release")
	}
	releaseStandalonePath(second.a, two, path)
}

func TestStandalonePathClaimCanonicalizesSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	first, second := newHarness(t, ""), newHarness(t, "")
	one := &doc{path: "link"}
	two := &doc{path: "target"}
	if !claimStandalonePath(first.a, one, link) {
		t.Fatal("could not claim symlink")
	}
	defer releaseStandalonePath(first.a, one, link)
	if claimStandalonePath(second.a, two, target) {
		releaseStandalonePath(second.a, two, target)
		t.Fatal("symlink and target received different owners")
	}
}

func TestStandaloneWriteReservationSurvivesWindowClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "saving.txt")
	first, second := newHarness(t, ""), newHarness(t, "")
	one := &doc{path: "saving"}
	two := &doc{path: "other"}
	if !reserveStandalonePath(first.a, one, path) {
		t.Fatal("could not reserve in-flight save")
	}
	releaseStandalonePaths(first.a)
	if claimStandalonePath(second.a, two, path) {
		releaseStandalonePath(second.a, two, path)
		t.Fatal("window close released an in-flight save")
	}
	// A stale save completion releases the reservation after its disk write.
	releaseStandalonePath(first.a, one, path)
	if !claimStandalonePath(second.a, two, path) {
		t.Fatal("completed stale save kept its reservation")
	}
	releaseStandalonePath(second.a, two, path)
}

func TestFolderlessWindowsDoNotOpenSameSavedFileTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	first := newHarness(t, "")
	d := first.a.openFile(path, nil)
	first.settle(func() bool { return d != nil && !d.loading })

	second := newHarness(t, "")
	revealed := false
	second.host.revealFile = func(got string) bool {
		revealed = workspace.DocPath("", "", got) == path && first.a.docs[path] != nil
		return revealed
	}
	if got := second.a.openFile(path, nil); got != nil {
		t.Fatal("direct folderless open bypassed cross-window ownership")
	}
	if !revealed || len(second.a.docs) != 0 {
		t.Fatalf("duplicate direct open: revealed=%v docs=%d", revealed, len(second.a.docs))
	}

	revealed = false
	second.host.pickFile = func() (string, error) { return path, nil }
	second.a.openFileDialog()
	second.settle(func() bool { return revealed })
	if len(second.a.docs) != 0 {
		t.Fatalf("second folderless window opened %d duplicate files", len(second.a.docs))
	}

	second.a.newFile()
	draft := second.a.docs[second.a.activePath()]
	draft.buf.Set("replacement")
	second.host.newFile = func(string, string) (string, error) { return path, nil }
	second.a.saveActive()
	second.settle(func() bool { return !draft.saving })
	if !draft.untitled || draft.buf.String() != "replacement" {
		t.Fatalf("blocked save changed draft: untitled=%v text=%q", draft.untitled, draft.buf.String())
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Fatalf("blocked save changed destination: text=%q err=%v", data, err)
	}
}

func TestSecondDraftNameIsSavePanelDefault(t *testing.T) {
	h := newHarness(t, "")
	h.a.newFile()
	h.a.newFile()
	var gotRoot, gotName string
	h.host.newFile = func(root, name string) (string, error) {
		gotRoot, gotName = root, name
		return "", nil
	}
	d := h.a.docs[h.a.activePath()]
	h.a.saveActive()
	h.settle(func() bool { return !d.saving })
	if gotRoot != "" || gotName != "Untitled-1" {
		t.Fatalf("save panel default = root %q name %q", gotRoot, gotName)
	}

	root := filepath.Join(t.TempDir(), "project")
	opts := newFileSaveOptions(nil, root, gotName)
	if opts.DefaultPath != filepath.Join(root, "Untitled-1") || opts.ButtonLabel == "" || opts.Title == "" {
		t.Fatalf("save options = %+v", opts)
	}
}

func TestNewFileCacheFailureKeepsWelcome(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	seeCache := filepath.Join(cache, "See")
	if err := os.RemoveAll(seeCache); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(seeCache), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seeCache, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(seeCache) })

	h.a.newFile()
	if len(h.a.docs) != 0 || !h.a.showWelcome {
		t.Fatalf("failed cache creation: docs=%d showWelcome=%v", len(h.a.docs), h.a.showWelcome)
	}
	if _, isErr := h.feedbackError(); h.a.welcomeNote == "" || !isErr {
		t.Fatal("failed cache creation did not expose the existing error feedback")
	}
}
