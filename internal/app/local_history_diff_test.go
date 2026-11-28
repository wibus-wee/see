package app

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/localhistory"
)

func TestLocalHistorySelectedLineMapping(t *testing.T) {
	tests := []struct {
		name      string
		old       string
		current   string
		selection editor.DiffLineRange
		want      string
	}{
		{
			name:      "subset of replacement hunk",
			old:       "head\nold one\nold two\ntail\n",
			current:   "head\nnew one\nnew two\ntail\n",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1},
			want:      "head\nold one\nnew two\ntail\n",
		},
		{
			name:      "historical deletion",
			old:       "head\nrestore me\ntail\n",
			current:   "head\ntail\n",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1},
			want:      "head\nrestore me\ntail\n",
		},
		{
			name:      "current insertion",
			old:       "head\ntail\n",
			current:   "head\nremove me\ntail\n",
			selection: editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1},
			want:      "head\ntail\n",
		},
		{
			name:      "unequal hunk old-only tail",
			old:       "head\nold one\nold two\ntail\n",
			current:   "head\nnew one\ntail\n",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 2, End: 2},
			want:      "head\nnew one\nold two\ntail\n",
		},
		{
			name:      "unequal hunk new-only tail",
			old:       "head\nold one\ntail\n",
			current:   "head\nnew one\nnew two\ntail\n",
			selection: editor.DiffLineRange{Side: editor.DiffNew, Start: 2, End: 2},
			want:      "head\nnew one\ntail\n",
		},
		{
			name:      "context only",
			old:       "context\nold\ntail\n",
			current:   "context\nnew\ntail\n",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 0, End: 0},
			want:      "context\nnew\ntail\n",
		},
		{
			name:      "second hunk uses document coordinates",
			old:       "zero\nold one\nmiddle\nold two\nend\n",
			current:   "zero\nnew one\nmiddle\nnew two\nend\n",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 3, End: 3},
			want:      "zero\nnew one\nmiddle\nold two\nend\n",
		},
		{
			name:      "unicode and EOF without final newline",
			old:       "head\n旧尾",
			current:   "head\nnew尾",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1},
			want:      "head\n旧尾",
		},
		{
			name:      "old-only EOF tail without final newline",
			old:       "head\npaired\nold tail",
			current:   "head\npaired",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 2, End: 2},
			want:      "head\npaired\nold tail",
		},
		{
			name:      "new-only final line keeps final newline",
			old:       "head\n",
			current:   "head\nadded\n",
			selection: editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1},
			want:      "head\n",
		},
		{
			name:      "old final newline only",
			old:       "abc\n",
			current:   "abc",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 0, End: 0},
			want:      "abc\n",
		},
		{
			name:      "new final newline only",
			old:       "abc",
			current:   "abc\n",
			selection: editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 0},
			want:      "abc",
		},
		{
			name:      "restore newline into empty file",
			old:       "abc\n",
			current:   "",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 0, End: 0},
			want:      "abc\n",
		},
		{
			name:      "old EOF tail with final newline",
			old:       "head\npaired\nold tail\n",
			current:   "head\npaired",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 2, End: 2},
			want:      "head\npaired\nold tail\n",
		},
		{
			name:      "remove final added line without final newline",
			old:       "head",
			current:   "head\nadded",
			selection: editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1},
			want:      "head",
		},
		{
			name:      "remove all lines",
			old:       "",
			current:   "first\nsecond\n",
			selection: editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 1},
			want:      "",
		},
		{
			name:      "paired and tail old lines together",
			old:       "a\nold\nextra\nend\n",
			current:   "a\nnew\nend\n",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 2},
			want:      "a\nold\nextra\nend\n",
		},
		{
			name:      "paired and tail at EOF with final newline",
			old:       "a\nb\n",
			current:   "c",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 0, End: 1},
			want:      "a\nb\n",
		},
		{
			name:      "paired and tail at EOF without final newline",
			old:       "a\nb",
			current:   "c",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 0, End: 1},
			want:      "a\nb",
		},
		{
			name:      "tail at EOF after unselected paired line with final newline",
			old:       "a\nb\n",
			current:   "c",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1},
			want:      "c\nb\n",
		},
		{
			name:      "tail at EOF after unselected paired line without final newline",
			old:       "a\nb",
			current:   "c",
			selection: editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1},
			want:      "c\nb",
		},
		{
			name:      "paired and added tail removed together",
			old:       "a\nold",
			current:   "a\nnew\nadded",
			selection: editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 2},
			want:      "a\nold",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := localHistoryTestDiff(test.old, test.current)
			state.SetSelectedLines(test.selection, true)
			edits := localHistorySelectedEdits(state)
			buffer := editor.NewBuffer(test.current)
			if len(edits) > 0 && !applyLocalHistoryEdits(buffer, buffer.Version(), edits) {
				t.Fatal("edits were rejected")
			}
			if got := buffer.String(); got != test.want {
				t.Fatalf("restored text = %q, want %q (hunks=%+v edits=%+v)", got, test.want, state.Hunks(), edits)
			}
			if test.name == "context only" && len(edits) != 0 {
				t.Fatalf("context selection produced edits: %+v", edits)
			}
		})
	}
}

func TestLocalHistoryCRLFRestoreIsOneUndoStep(t *testing.T) {
	current := "head\r\nnew 尾\r\nend"
	buffer := editor.NewBuffer(current)
	state := localHistoryTestDiff("head\n旧 尾\nend", buffer.String())
	state.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1}, true)
	version := buffer.Version()
	if !applyLocalHistoryEdits(buffer, version, localHistorySelectedEdits(state)) {
		t.Fatal("restore failed")
	}
	if got, want := buffer.String(), "head\n旧 尾\nend"; got != want {
		t.Fatalf("CRLF restore = %q, want %q", got, want)
	}
	if buffer.LineEnding() != "\r\n" {
		t.Fatalf("line ending = %q, want CRLF", buffer.LineEnding())
	}
	if !buffer.Undo() {
		t.Fatal("restore was not undoable")
	}
	if got := buffer.String(); got != "head\nnew 尾\nend" {
		t.Fatalf("undo = %q", got)
	}
	if buffer.Undo() {
		t.Fatal("restore was split into more than one undo step")
	}
}

func TestLocalHistoryRejectsStaleBufferVersion(t *testing.T) {
	buffer := editor.NewBuffer("head\nnew\ntail\n")
	state := localHistoryTestDiff("head\nold\ntail\n", buffer.String())
	state.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1}, true)
	edits := localHistorySelectedEdits(state)
	version := buffer.Version()
	buffer.Edit(editor.Edit{Start: 0, End: 0, Text: "changed\n"})
	before := buffer.String()
	if applyLocalHistoryEdits(buffer, version, edits) {
		t.Fatal("stale target accepted edits")
	}
	if got := buffer.String(); got != before {
		t.Fatalf("stale restore changed buffer to %q", got)
	}
}

func TestLocalHistoryRestoreMethodRejectsStaleTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("head\nnew\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	installLocalHistoryStore(t, h, store)
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(s []localhistory.Snapshot) bool { return len(s) == 1 })
	if _, _, err := store.Add(path, "head\nold\n", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	v := openLocalHistoryDiffForTest(t, h, d)
	v.localHistory.state.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1}, true)
	d.buf.Edit(editor.Edit{Start: d.buf.Len(), End: d.buf.Len(), Text: "later\n"})
	before := d.buf.String()
	h.a.restoreLocalHistorySelected(h.a.focus, localHistoryTab(path), v)
	if d.buf.String() != before || h.a.localHistoryTargetCurrent(v.localHistory) {
		t.Fatal("restore accepted a stale source buffer")
	}
}

func TestLocalHistoryCapturesUnsavedDeletionOutsideGit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.go")
	disk := "package main\nfunc recoverMe() {}\n"
	if err := os.WriteFile(path, []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	storeDir := filepath.Join(t.TempDir(), "history")
	store := localhistory.New(storeDir)
	installLocalHistoryStore(t, h, store)
	d := h.open("source.go")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) == 1 && snapshots[0].Text == disk
	})
	start := strings.Index(d.buf.String(), "func recoverMe() {}\n")
	d.buf.Edit(editor.Edit{Start: start, End: start + len("func recoverMe() {}\n")})
	d.localHistorySaved = time.Now().Add(-localHistoryCheckpoint)
	h.a.captureEditedLocalHistory(d)
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) >= 2 && snapshots[0].Text == "package main\n"
	})
	if got, err := os.ReadFile(path); err != nil || string(got) != disk {
		t.Fatalf("capturing unsaved deletion changed disk file: text=%q err=%v", got, err)
	}
	reopened := localhistory.New(storeDir)
	snapshots, err := reopened.List(path)
	if err != nil || len(snapshots) < 2 || snapshots[len(snapshots)-1].Text != disk {
		t.Fatalf("persisted snapshots = %+v, err=%v", snapshots, err)
	}
}

func TestLocalHistoryDebouncesUnsavedVersions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("start"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	installLocalHistoryStore(t, h, store)
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool { return len(snapshots) == 1 })
	for _, suffix := range []string{" one", " two", " three"} {
		end := d.buf.Len()
		d.buf.Edit(editor.Edit{Start: end, End: end, Text: suffix})
		h.a.captureEditedLocalHistory(d)
	}
	want := "start one two three"
	waitForLocalHistoryAndDispatch(t, h, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) >= 2 && snapshots[0].Text == want
	})
	snapshots, err := store.List(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("debounced snapshots = %d, want initial plus final", len(snapshots))
	}
}

func TestLocalHistoryDiffTabLifecycleAndSession(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.go")
	current := "package p\nfunc value() int { return 2 }\n"
	if err := os.WriteFile(path, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	installLocalHistoryStore(t, h, store)
	d := h.open("source.go")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool { return len(snapshots) == 1 })
	if _, _, err := store.Add(path, "package p\nfunc value() int { return 1 }\n", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	v := openLocalHistoryDiffForTest(t, h, d)
	key := localHistoryTab(path)
	originalGroup := h.a.focus
	if !isDiffTab(originalGroup.active) || originalGroup.active != key || v.localHistory.state == nil || !v.localHistory.state.LineSelection {
		t.Fatalf("history did not open as a native diff tab: active=%q view=%+v", originalGroup.active, v.localHistory)
	}
	if h.a.tabFile(originalGroup, key) != path || diffTabSpec(v) != nil {
		t.Fatal("history tab is not associated with its source file or was treated as Git")
	}
	firstState := v.state
	firstState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1}, true)
	h.a.splitActive(sideRight)
	var other *group
	for _, g := range h.a.groups() {
		if g != originalGroup {
			other = g
		}
	}
	if other == nil {
		t.Fatal("splitting the history diff did not create a group")
	}
	clone := other.diffs[key]
	if clone == nil || clone.localHistory == v.localHistory || clone.state == firstState {
		t.Fatal("split history diff shares its view or DiffState")
	}
	if _, selected := clone.state.SelectedLines(); selected {
		t.Fatal("split inherited the original line selection")
	}
	if spec := diffTabSpec(clone); spec != nil {
		t.Fatalf("local history was saved as a Git diff: %+v", spec)
	}
	h.a.closeTab(other, key)
	if h.a.docs[path] != d {
		t.Fatal("closing the history tab released the source document")
	}
	closeText := d.buf.String() + "// captured on close\n"
	d.buf.Set(closeText)
	d.buf.MarkSaved()
	h.a.closeTab(originalGroup, path)
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		for _, snapshot := range snapshots {
			if snapshot.Text == closeText {
				return true
			}
		}
		return false
	})
	if h.a.docs[path] != nil || h.a.localHistoryTargetCurrent(v.localHistory) {
		t.Fatal("closing the source left a valid restore target")
	}
}

func TestLocalHistoryTesterRestoresSelectedDiffLinesAndScreenshots(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "account.go")
	disk := `package account

import "strings"

type Account struct {
	ID int64
	Name string
}

func normalizedName(name string) string {
	name = strings.TrimSpace(name)
	return strings.ToLower(name)
}

func accountLabel(account Account) string {
	return account.Name
}
`
	current := strings.Replace(disk,
		"\tname = strings.TrimSpace(name)\n\treturn strings.ToLower(name)",
		"\tname = strings.TrimSpace(strings.ToLower(name))\n\treturn strings.ToUpper(name)", 1)
	historical := disk
	older := strings.Replace(historical, "return strings.ToLower(name)", "return strings.ToTitle(name)", 1)
	if err := os.WriteFile(path, []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	h.tt.SetSize(1280, 800)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	installLocalHistoryStore(t, h, store)
	d := h.open("account.go")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) == 1 && snapshots[0].Text == disk
	})
	d.buf.Set(current)
	base := time.Now()
	if _, _, err := store.Add(path, older, base.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Add(path, historical, base.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	v := openLocalHistoryDiffForTest(t, h, d)
	index := -1
	for i, snapshot := range v.localHistory.snapshots {
		if snapshot.Text == historical {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("historical snapshot is missing")
	}
	a := h.a
	a.selectLocalHistorySnapshot(v, index)
	state := v.localHistory.state
	oldLine := strings.Index(historical, "\tname = strings.TrimSpace(name)")
	if oldLine < 0 {
		t.Fatal("historical changed line is missing")
	}
	line := strings.Count(historical[:oldLine], "\n")
	st := a.focus.eds[path]
	st.SetSelections(editor.Selection{Anchor: d.buf.Len(), Head: d.buf.Len()})
	h.tt.Frame()
	oldHeader, ok := h.tt.Find("Old text")
	if !ok {
		t.Fatalf("old diff pane is not visible: %q", h.tt.Texts())
	}
	h.tt.ClickAt(oldHeader.X+4, oldHeader.Y+101)
	selected, ok := state.SelectedLines()
	if !ok || selected != (editor.DiffLineRange{Side: editor.DiffOld, Start: line, End: line}) {
		t.Fatalf("gutter click selected %+v, want old line %d", selected, line)
	}
	if len(localHistorySelectedEdits(state)) != 1 {
		t.Fatalf("selected historical line did not produce one edit: %+v", localHistorySelectedEdits(state))
	}
	key := localHistoryTab(path)
	if a.focus.active != key || a.focus.diffs[key] != v || v.state != state || !state.LineSelection {
		t.Fatal("local history is not rendered as the active native diff tab")
	}
	if !h.tt.HasText(localHistoryText("localHistoryTab")) || !h.tt.HasText(localHistoryText("localHistoryRestoreSelected")) {
		t.Fatalf("diff controls are missing: %v", h.tt.Texts())
	}
	if !h.tt.HasText("account.go (" + localHistoryText("localHistoryTab") + ")") {
		t.Fatalf("history tab does not distinguish its source file: %v", h.tt.Texts())
	}
	if h.tt.HasText("Set the destination") || h.tt.HasText("Insert at line") {
		t.Fatalf("rejected destination-cursor UI remains visible: %v", h.tt.Texts())
	}
	if !h.tt.HasText(filepath.Base(root)) || !h.tt.HasText("account.go") {
		t.Fatalf("workspace chrome or file label is missing: %v", h.tt.Texts())
	}
	saveLocalHistoryScreenshot(t, os.Getenv("SEE_LOCAL_HISTORY_WORKING_TREE_SCREENSHOT"), h)
	if got := st.Selection(); got.Start() != d.buf.Len() || got.End() != d.buf.Len() {
		t.Fatalf("source caret changed before restore: %+v", got)
	}
	if err := h.tt.Click(localHistoryText("localHistoryRestoreSelected")); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(current,
		"\tname = strings.TrimSpace(strings.ToLower(name))",
		"\tname = strings.TrimSpace(name)", 1)
	if got := d.buf.String(); got != want {
		t.Fatalf("restore ignored diff coordinates or changed unselected lines:\n%s\nwant:\n%s", got, want)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != disk {
		t.Fatalf("restore saved the file: text=%q err=%v", got, err)
	}
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		foundBefore, foundAfter := false, false
		for _, snapshot := range snapshots {
			foundBefore = foundBefore || snapshot.Text == current
			foundAfter = foundAfter || snapshot.Text == want
		}
		return foundBefore && foundAfter
	})
	if err := h.tt.Click(localHistoryText("localHistoryOpenFile")); err != nil {
		t.Fatal(err)
	}
	if a.focus.active != path {
		t.Fatalf("Open File activated %q, want %q", a.focus.active, path)
	}
	if !d.buf.Undo() {
		t.Fatal("restore was not undoable in the source editor")
	}
	if got := d.buf.String(); got != current {
		t.Fatalf("undo = %q, want %q", got, current)
	}
}

func TestLocalHistoryTesterSelectsLinesByGutterInSplitAndUnifiedDiff(t *testing.T) {
	for _, unified := range []bool{false, true} {
		name := "split"
		if unified {
			name = "unified"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "source.txt")
			old := "head\nold one\nold two\ntail\n"
			current := "head\nnew one\nnew two\ntail\n"
			if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
				t.Fatal(err)
			}
			h := newHarness(t, root)
			h.tt.SetSize(1280, 800)
			h.a.cfg.DiffUnified = unified
			store := localhistory.New(filepath.Join(t.TempDir(), "history"))
			installLocalHistoryStore(t, h, store)
			d := h.open("source.txt")
			waitForLocalHistory(t, store, path, func(s []localhistory.Snapshot) bool { return len(s) == 1 })
			d.buf.Set(current)
			v := openLocalHistoryDiffForTest(t, h, d)
			h.tt.Frame()
			oldHeader, ok := h.tt.Find("Old text")
			if !ok {
				oldHeader, ok = h.tt.Find("New text")
			}
			if !ok {
				t.Fatalf("diff pane is not visible: %q", h.tt.Texts())
			}
			x1, y1 := oldHeader.X+4, oldHeader.Y+36
			x2, y2 := oldHeader.X+4, oldHeader.Y+55
			h.tt.Press(x1, y1)
			h.tt.Move(x2, y2)
			h.tt.Release(x2, y2)
			h.tt.ClickAtWith(ui.Shift, x2, y2)
			selected, ok := v.localHistory.state.SelectedLines()
			if !ok || selected != (editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 2}) {
				t.Fatalf("gutter drag/shift-click selected %+v, want old lines 1..2", selected)
			}
		})
	}
}

func TestLocalHistoryLoadingTabsSurviveSplitAndMove(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	installLocalHistoryStore(t, h, store)
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(s []localhistory.Snapshot) bool { return len(s) == 1 })
	main := h.a.focus
	h.a.showLocalHistory(main, d)
	key := localHistoryTab(path)
	h.a.splitActive(sideRight)
	var cloneGroup *group
	for _, g := range h.a.groups() {
		if g != main && g.diffs[key] != nil {
			cloneGroup = g
		}
	}
	if cloneGroup == nil {
		t.Fatal("split did not clone the loading history tab")
	}
	clone := cloneGroup.diffs[key]
	target := h.a.splitBeside(cloneGroup, sideRight)
	h.a.moveTab(cloneGroup, key, target, sideNone, 0)
	h.settle(func() bool {
		return !main.diffs[key].localHistory.loading && !target.diffs[key].localHistory.loading
	})
	if target.diffs[key].localHistory.state == nil || main.diffs[key].localHistory.state == nil {
		t.Fatal("split or moved history tab did not finish loading")
	}
	if target.diffs[key].localHistory == main.diffs[key].localHistory || clone.localHistory == main.diffs[key].localHistory {
		t.Fatal("split or moved history tab shares mutable view state")
	}
	mainView := main.diffs[key]
	h.a.activateIn(main, key)
	h.a.refreshLocalHistory(main, key, mainView)
	h.a.splitActive(sideRight)
	var refreshCloneGroup *group
	for _, g := range h.a.groups() {
		if g != main && g.diffs[key] != nil && g.diffs[key].localHistory != target.diffs[key].localHistory {
			refreshCloneGroup = g
		}
	}
	if refreshCloneGroup == nil {
		t.Fatal("split did not clone the refreshing history tab")
	}
	refreshClone := refreshCloneGroup.diffs[key]
	refreshTarget := h.a.splitBeside(refreshCloneGroup, sideRight)
	h.a.moveTab(refreshCloneGroup, key, refreshTarget, sideNone, 0)
	h.settle(func() bool {
		return !mainView.localHistory.loading && !refreshTarget.diffs[key].localHistory.loading
	})
	if mainView.localHistory.state == nil || refreshTarget.diffs[key].localHistory.state == nil || refreshClone.localHistory == mainView.localHistory {
		t.Fatal("split or moved refreshing tab did not finish with independent state")
	}
}

func TestLocalHistorySnapshotPickerClearsSelection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	installLocalHistoryStore(t, h, store)
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(s []localhistory.Snapshot) bool { return len(s) == 1 })
	if _, _, err := store.Add(path, "older\n", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	v := openLocalHistoryDiffForTest(t, h, d)
	if len(v.localHistory.snapshots) < 2 {
		t.Fatal("test requires multiple snapshots")
	}
	h.tt.Frame()
	v.state.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffOld, Start: 0, End: 0}, true)
	oldState := v.state
	targetIndex := 1
	target := v.localHistory.snapshots[targetIndex]
	currentLabel := v.localHistory.snapshots[v.localHistory.selected].CreatedAt.Local().Format("2006-01-02 15:04:05")
	if err := h.tt.Click(currentLabel); err != nil {
		t.Fatal(err)
	}
	menuLabel := target.CreatedAt.Local().Format("2006-01-02 15:04:05") + "  ·  " + localHistoryFormat("localHistorySnapshotLines", map[string]any{"lines": localHistoryLineCount(target.Text)})
	if err := h.tt.Click(menuLabel); err != nil {
		t.Fatal(err)
	}
	if v.localHistory.selected != targetIndex || v.state == oldState {
		t.Fatalf("snapshot picker did not select new comparison: selected=%d", v.localHistory.selected)
	}
	if _, ok := v.state.SelectedLines(); ok {
		t.Fatal("snapshot change retained the previous line selection")
	}
}

func TestLocalHistoryEmptyAndLoadErrorStates(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "source.txt")
		if err := os.WriteFile(path, []byte("text\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		h := newHarness(t, root)
		store := localhistory.New(filepath.Join(t.TempDir(), "initial"))
		installLocalHistoryStore(t, h, store)
		d := h.open("source.txt")
		waitForLocalHistory(t, store, path, func(s []localhistory.Snapshot) bool { return len(s) == 1 })
		h.a.localHistoryStore = localhistory.New(filepath.Join(t.TempDir(), "empty"))
		h.a.showLocalHistory(h.a.focus, d)
		v := h.a.focus.diffs[localHistoryTab(path)]
		h.settle(func() bool { return v != nil && !v.localHistory.loading })
		if !h.tt.HasText(localHistoryText("localHistoryNoSnapshots")) || v.localHistory.state != nil {
			t.Fatalf("empty history state not rendered: %v", h.tt.Texts())
		}
	})
	t.Run("error", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "source.txt")
		if err := os.WriteFile(path, []byte("text\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		h := newHarness(t, root)
		d := h.open("source.txt")
		h.a.localHistoryStore = nil
		h.a.localHistoryErr = errors.New("history unavailable")
		h.a.showLocalHistory(h.a.focus, d)
		v := h.a.focus.diffs[localHistoryTab(path)]
		h.tt.Frame()
		if v.localHistory.loadErr == nil || !h.tt.HasText("history unavailable") {
			t.Fatalf("history load error was not rendered: %v", h.tt.Texts())
		}
		before := d.buf.String()
		h.a.restoreLocalHistorySelected(h.a.focus, localHistoryTab(path), v)
		if d.buf.String() != before {
			t.Fatal("restore ran while history loading failed")
		}
	})
}

func localHistoryTestDiff(old, current string) *editor.DiffState {
	return &editor.DiffState{Old: editor.NewBuffer(old), New: editor.NewBuffer(current), LineSelection: true}
}

func openLocalHistoryDiffForTest(t *testing.T, h *harness, d *doc) *gitDiffTab {
	t.Helper()
	h.a.showLocalHistory(h.a.focus, d)
	key := localHistoryTab(d.path)
	h.settle(func() bool {
		v := h.a.focus.diffs[key]
		return v != nil && v.localHistory != nil && !v.localHistory.loading
	})
	v := h.a.focus.diffs[key]
	if v == nil || v.localHistory == nil || v.localHistory.loadErr != nil {
		t.Fatalf("local history diff failed to load: %+v", v)
	}
	return v
}

func saveLocalHistoryScreenshot(t *testing.T, path string, h *harness) {
	t.Helper()
	if path == "" {
		return
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, h.tt.Image()); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("Screenshot: %s", path)
}

// installLocalHistoryStore points the harnessed app at store and drains
// the writes its captures start before the test tears down. The drain
// registers here, after the store's temporary directory exists, so it
// runs before that directory is removed: a capture write landing in the
// removal window recreates a directory the cleanup already scanned, and
// the removal fails with the directory not empty. Debounced captures
// cannot start a write afterwards — the queue that would run them is
// never pumped again — so the started writes are everything to drain.
func installLocalHistoryStore(t *testing.T, h *harness, store *localhistory.Store) {
	t.Helper()
	h.a.localHistoryStore = store
	t.Cleanup(h.a.localHistoryWrites.Wait)
}

func waitForLocalHistory(t *testing.T, store *localhistory.Store, path string, ready func([]localhistory.Snapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshots, err := store.List(path)
		if err != nil {
			t.Fatal(err)
		}
		if ready(snapshots) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("local history did not reach the expected state")
}

func waitForLocalHistoryAndDispatch(t *testing.T, h *harness, store *localhistory.Store, path string, ready func([]localhistory.Snapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case fn := <-h.q:
			fn()
		default:
		}
		snapshots, err := store.List(path)
		if err != nil {
			t.Fatal(err)
		}
		if ready(snapshots) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("local history did not reach the expected state")
}

func corruptLocalHistoryRecord(t *testing.T, storeDir, oldText, newText string) {
	t.Helper()
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := filepath.Join(storeDir, entry.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		mutated := strings.Replace(string(data), oldText, newText, 1)
		if mutated != string(data) {
			if err := os.WriteFile(name, []byte(mutated), 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no history record contains %q", oldText)
}

func TestLocalHistoryCorruptSnapshotNeverReachesBuffer(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	disk := "head\ncurrent\n"
	if err := os.WriteFile(path, []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	storeDir := filepath.Join(t.TempDir(), "history")
	store := localhistory.New(storeDir)
	installLocalHistoryStore(t, h, store)
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(s []localhistory.Snapshot) bool { return len(s) == 1 })
	if _, _, err := store.Add(path, "head\ntampered-original\n", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Valid JSON, wrong bytes: the stored text no longer matches the
	// digest written with it.
	corruptLocalHistoryRecord(t, storeDir, "tampered-original", "forged!!-original")
	v := openLocalHistoryDiffForTest(t, h, d)
	corruptIndex, cleanIndex := -1, -1
	for i, snapshot := range v.localHistory.snapshots {
		if snapshot.Corrupt {
			corruptIndex = i
		} else {
			cleanIndex = i
		}
	}
	if corruptIndex < 0 {
		t.Fatalf("corrupt snapshot is missing or unflagged: %+v", v.localHistory.snapshots)
	}
	h.a.selectLocalHistorySnapshot(v, corruptIndex)
	if v.localHistory.state != nil {
		t.Fatal("a corrupt snapshot must never become a restorable diff")
	}
	before := d.buf.String()
	h.a.restoreLocalHistorySelected(h.a.focus, localHistoryTab(path), v)
	if d.buf.String() != before {
		t.Fatalf("corrupt snapshot bytes reached the buffer: %q", d.buf.String())
	}
	h.tt.Frame()
	if !h.tt.HasText("corrupt") {
		t.Fatalf("corruption is not surfaced in the UI: %v", h.tt.Texts())
	}
	if strings.Contains(d.buf.String(), "forged") {
		t.Fatal("forged bytes reached the buffer")
	}
	// The intact snapshot still restores, proving corruption blocks only
	// the corrupted entry.
	if cleanIndex < 0 {
		t.Fatal("clean snapshot missing")
	}
	h.a.selectLocalHistorySnapshot(v, cleanIndex)
	if v.localHistory.state == nil {
		t.Fatal("clean snapshot lost its diff")
	}
}

func TestLocalHistoryLegacySnapshotWithoutDigestRestores(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	disk := "head\nnew\n"
	if err := os.WriteFile(path, []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	storeDir := filepath.Join(t.TempDir(), "history")
	store := localhistory.New(storeDir)
	installLocalHistoryStore(t, h, store)
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(s []localhistory.Snapshot) bool { return len(s) == 1 })
	if _, _, err := store.Add(path, "head\nold\n", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Strip every digest field: the record now looks like one written
	// before digests existed, valid and unverifiable.
	entries, err := os.ReadDir(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := filepath.Join(storeDir, entry.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var stripped strings.Builder
		rest := string(data)
		for {
			idx := strings.Index(rest, `,"digest":"`)
			if idx < 0 {
				stripped.WriteString(rest)
				break
			}
			stripped.WriteString(rest[:idx])
			end := strings.Index(rest[idx+len(`,"digest":"`):], `"`)
			rest = rest[idx+len(`,"digest":"`)+end+1:]
		}
		if err := os.WriteFile(name, []byte(stripped.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	v := openLocalHistoryDiffForTest(t, h, d)
	legacyIndex := -1
	for i, snapshot := range v.localHistory.snapshots {
		if snapshot.Text == "head\nold\n" {
			if snapshot.Corrupt {
				t.Fatal("legacy snapshot must not be flagged corrupt")
			}
			legacyIndex = i
		}
	}
	if legacyIndex < 0 {
		t.Fatalf("legacy snapshot is missing: %+v", v.localHistory.snapshots)
	}
	h.a.selectLocalHistorySnapshot(v, legacyIndex)
	v.localHistory.state.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1}, true)
	h.a.restoreLocalHistorySelected(h.a.focus, localHistoryTab(path), v)
	if got := d.buf.String(); got != "head\nold\n" {
		t.Fatalf("legacy restore = %q, want %q", got, "head\nold\n")
	}
}
