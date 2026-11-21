package app

import (
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/localhistory"
	"see/locales"
)

func TestReplaceLocalHistorySelectionPreservesTextAndUndo(t *testing.T) {
	buf := editor.NewBuffer("keep 😀\r\nremove\r\n尾")
	before := buf.String()
	start := strings.Index(before, "remove")
	end := start + len("remove")
	version := buf.Version()
	if !replaceLocalHistorySelection(buf, version, start, end, "恢复") {
		t.Fatal("replacement was rejected")
	}
	want := "keep 😀\n恢复\n尾"
	if got := buf.String(); got != want {
		t.Fatalf("restored text = %q, want %q", got, want)
	}
	if buf.LineEnding() != "\r\n" {
		t.Fatalf("line ending = %q, want CRLF", buf.LineEnding())
	}
	if !buf.Undo() {
		t.Fatal("restore was not undoable")
	}
	if got := buf.String(); got != before {
		t.Fatalf("undo = %q, want %q", got, before)
	}
}

func TestReplaceLocalHistorySelectionSupportsEmptyAndEndOfFile(t *testing.T) {
	buf := editor.NewBuffer("first\nremove\nlast")
	text := buf.String()
	start := strings.Index(text, "remove")
	if !replaceLocalHistorySelection(buf, buf.Version(), start, start+len("remove\n"), "") {
		t.Fatal("empty replacement was rejected")
	}
	if got, want := buf.String(), "first\nlast"; got != want {
		t.Fatalf("empty replacement = %q, want %q", got, want)
	}
	end := buf.Len()
	if !replaceLocalHistorySelection(buf, buf.Version(), end, end, "\n尾") {
		t.Fatal("EOF insertion was rejected")
	}
	if got, want := buf.String(), "first\nlast\n尾"; got != want {
		t.Fatalf("EOF insertion = %q, want %q", got, want)
	}
}

func TestReplaceLocalHistorySelectionRejectsStaleVersion(t *testing.T) {
	buf := editor.NewBuffer("keep old tail")
	version := buf.Version()
	buf.Edit(editor.Edit{Start: 0, End: 0, Text: "new "})
	before := buf.String()
	if replaceLocalHistorySelection(buf, version, 5, 8, "restored") {
		t.Fatal("stale replacement was accepted")
	}
	if got := buf.String(); got != before {
		t.Fatalf("stale replacement changed text to %q", got)
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
	h.a.localHistoryStore = store
	d := h.open("source.go")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) == 1 && snapshots[0].Text == disk
	})

	start := strings.Index(d.buf.String(), "func recoverMe() {}\n")
	d.buf.Edit(editor.Edit{Start: start, End: start + len("func recoverMe() {}\n"), Text: ""})
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
	h.a.localHistoryStore = store
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
		t.Fatalf("debounced snapshots = %d, want initial plus final, got %+v", len(snapshots), snapshots)
	}
}

func TestLocalHistoryRestoresOnlySelectedTextAndUndo(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	disk := "one\nbaseline value\nthree\n"
	if err := os.WriteFile(path, []byte(disk), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	h.a.localHistoryStore = store
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) == 1 && snapshots[0].Text == disk
	})
	historical := "historic top\noriginal value\nthree\n"
	if _, _, err := store.Add(path, historical, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	current := "one\nnew value\nthree\n"
	d.buf.Set(current)
	st := h.a.focus.eds[path]
	start := strings.Index(current, "new")
	st.SetSelections(editor.Selection{Anchor: start, Head: start + len("new")})
	openLocalHistoryForTest(t, h, d)
	p := h.a.localHistoryPanel
	selectSnapshotText(t, h.a, p, historical)
	sourceStart := strings.Index(historical, "original")
	p.preview.SetSelections(editor.Selection{Anchor: sourceStart, Head: sourceStart + len("original")})
	h.a.restoreLocalHistory()

	want := "one\noriginal value\nthree\n"
	if got := d.buf.String(); got != want {
		t.Fatalf("restored buffer = %q, want %q", got, want)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != disk {
		t.Fatalf("restore changed disk file: text=%q err=%v", got, err)
	}
	if h.a.localHistoryPanel != nil {
		t.Fatal("restore did not close history")
	}
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		foundBefore, foundAfter := false, false
		for _, snapshot := range snapshots {
			foundBefore = foundBefore || snapshot.Text == current
			foundAfter = foundAfter || snapshot.Text == want
		}
		return foundBefore && foundAfter
	})
	if !d.buf.Undo() {
		t.Fatal("restore was not one undoable edit")
	}
	if got := d.buf.String(); got != current {
		t.Fatalf("undo = %q, want %q", got, current)
	}
}

func TestLocalHistoryRejectsStaleAndClosedTargets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("old text"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	h.a.localHistoryStore = store
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) == 1 && snapshots[0].Text == "old text"
	})
	if _, _, err := store.Add(path, "historical text", time.Now()); err != nil {
		t.Fatal(err)
	}
	openLocalHistoryForTest(t, h, d)
	p := h.a.localHistoryPanel
	selectSnapshotText(t, h.a, p, "historical text")
	p.preview.SetSelections(editor.Selection{Anchor: 0, Head: len("historical")})
	d.buf.Edit(editor.Edit{Start: 0, End: 0, Text: "new "})
	before := d.buf.String()
	h.a.restoreLocalHistory()
	if got := d.buf.String(); got != before {
		t.Fatalf("stale restore changed target to %q", got)
	}
	h.a.closeLocalHistory()
	d.buf.MarkSaved()

	openLocalHistoryForTest(t, h, d)
	p = h.a.localHistoryPanel
	selectSnapshotText(t, h.a, p, "historical text")
	p.preview.SetSelections(editor.Selection{Anchor: 0, Head: len("historical")})
	h.a.closeTab(h.a.focus, path)
	if h.a.docs[path] != nil {
		t.Fatal("closed document was retained")
	}
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		for _, snapshot := range snapshots {
			if snapshot.Text == before {
				return true
			}
		}
		return false
	})
	h.a.restoreLocalHistory()
	if p.buffer.String() != before {
		t.Fatalf("closed target was modified to %q", p.buffer.String())
	}
	if h.a.localHistoryPanel != p {
		t.Fatal("stale panel should remain available to close")
	}
}

func TestLocalHistoryPanelCanCloseAndReopenAcrossFrames(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	h.a.localHistoryStore = store
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) == 1 && snapshots[0].Text == "one"
	})
	if _, _, err := store.Add(path, "older", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	openLocalHistoryForTest(t, h, d)
	first := h.a.localHistoryPanel
	h.tt.Frame()
	h.a.closeLocalHistory()
	h.tt.Frame()
	h.tt.Frame()
	if h.a.localHistoryPanel != nil {
		t.Fatal("history panel remained open after close frames")
	}
	openLocalHistoryForTest(t, h, d)
	second := h.a.localHistoryPanel
	h.tt.Frame()
	h.tt.Frame()
	if second == nil || second == first || second.loading || len(second.snapshots) == 0 {
		t.Fatalf("reopened panel did not settle after extra frames: %+v", second)
	}
	h.a.closeLocalHistory()
}

func TestLocalHistoryPanelKeepsTypingOutOfSourceEditor(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.txt")
	if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	h.a.localHistoryStore = store
	d := h.open("source.txt")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) == 1 && snapshots[0].Text == "source"
	})
	if _, _, err := store.Add(path, "historical", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	openLocalHistoryForTest(t, h, d)
	before := d.buf.String()
	h.tt.Type("typed while history is open")
	h.tt.Frame()
	if got := d.buf.String(); got != before {
		t.Fatalf("typing in history panel changed source buffer to %q", got)
	}
	h.a.closeLocalHistory()
}

func TestLocalHistoryCountMessagesFormatInBothLocales(t *testing.T) {
	for _, test := range []struct {
		locale, key string
		args        map[string]any
		want        string
	}{
		{"en", "localHistoryLines", map[string]any{"lines": 3}, "3 lines"},
		{"zh-Hans", "localHistoryLines", map[string]any{"lines": 3}, "3 行"},
		{"en", "localHistorySelectionCount", map[string]any{"start": 2, "end": 4, "lines": 3, "characters": 25}, "Lines 2–4 · 3 lines · 25 characters selected"},
		{"zh-Hans", "localHistorySelectionCount", map[string]any{"start": 2, "end": 4, "lines": 3, "characters": 25}, "第 2–4 行 · 已选中 3 行、25 个字符"},
		{"en", "localHistoryInsertAtLine", map[string]any{"line": 12}, "Insert at line 12"},
		{"zh-Hans", "localHistoryInsertAtLine", map[string]any{"line": 12}, "插入到第 12 行"},
	} {
		if got := locales.Catalog.Format(test.locale, test.key, test.args); got != test.want {
			t.Errorf("%s/%s = %q, want %q", test.locale, test.key, got, test.want)
		}
	}
}

func TestLocalHistoryTesterSelectsAndRestoresPreviewText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "account.go")
	common := `package account

import (
	"context"
	"database/sql"
	"fmt"
)

type User struct {
	ID int64
	Name string
	Email string
}

`
	current := common + `func findUser(ctx context.Context, db *sql.DB, id int64) (User, error) {
	var user User
	err := db.QueryRowContext(ctx, "SELECT id, name, email FROM users WHERE id = ?", id).
		Scan(&user.ID, &user.Name, &user.Email)
	if err != nil {
		return User{}, fmt.Errorf("find user %d: %w", id, err)
	}
	return user, nil
}
`
	historical := common + `func restoreUser(ctx context.Context, db *sql.DB, user User) error {
	_, err := db.ExecContext(ctx, "UPDATE users SET name=?, email=? WHERE id=?",
		user.Name, user.Email, user.ID)
	if err != nil {
		return fmt.Errorf("restore user %d: %w", user.ID, err)
	}
	return nil
}

` + strings.TrimPrefix(current, common)
	older := common + `func validUser(user User) bool {
	return user.ID > 0 && user.Name != ""
}

` + strings.TrimPrefix(current, common)
	newer := historical + `
func userLabel(user User) string {
	return fmt.Sprintf("%s <%s>", user.Name, user.Email)
}
`
	if err := os.WriteFile(path, []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	h.tt.SetSize(1280, 800)
	store := localhistory.New(filepath.Join(t.TempDir(), "history"))
	h.a.localHistoryStore = store
	d := h.open("account.go")
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		return len(snapshots) == 1 && snapshots[0].Text == current
	})
	base := time.Now()
	for i, text := range []string{older, historical, newer} {
		if _, _, err := store.Add(path, text, base.Add(time.Duration(i-5)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	st := h.a.focus.eds[path]
	destStart := strings.Index(current, "func findUser")
	if destStart < 0 {
		t.Fatal("destination selection not found")
	}
	st.SetSelections(editor.Selection{Anchor: destStart, Head: destStart})
	destLine := strings.Count(current[:destStart], "\n") + 1
	openLocalHistoryForTest(t, h, d)
	p := h.a.localHistoryPanel
	var historicalAt time.Time
	for _, snapshot := range p.snapshots {
		if snapshot.Text == historical {
			historicalAt = snapshot.CreatedAt
			break
		}
	}
	if historicalAt.IsZero() {
		t.Fatal("historical code version is missing")
	}
	timestampLabel := historicalAt.Local().Format("2006-01-02 15:04:05")
	if err := h.tt.Click(timestampLabel); err != nil {
		t.Fatal(err)
	}
	if p.selected < 0 || p.selected >= len(p.snapshots) || p.snapshots[p.selected].Text != historical {
		t.Fatalf("UI selected wrong snapshot: index=%d", p.selected)
	}
	previewLabel := p.preview.Label
	if err := h.tt.Click(previewLabel); err != nil {
		t.Fatal(err)
	}
	restoreStart := strings.Index(historical, "func restoreUser")
	restoreEnd := strings.Index(historical, "func findUser")
	if restoreStart < 0 || restoreEnd <= restoreStart {
		t.Fatal("historical restoreUser function not found")
	}
	restoreFunction := strings.TrimRight(historical[restoreStart:restoreEnd], "\n")
	targetLine := strings.Count(historical[:restoreStart], "\n")
	docStartModifier := ui.Ctrl
	if runtime.GOOS == "darwin" {
		docStartModifier = ui.Super
	}
	h.tt.Key(docStartModifier, ui.KeyHome)
	for line := 0; line < targetLine; line++ {
		h.tt.Key(0, ui.KeyDown)
	}
	for line := 1; line < strings.Count(restoreFunction, "\n")+1; line++ {
		h.tt.Key(ui.Shift, ui.KeyDown)
	}
	h.tt.Key(ui.Shift, ui.KeyEnd)
	h.tt.Frame()
	selection := p.preview.Selection()
	source := p.preview.Buffer.Snapshot().String()[selection.Start():selection.End()]
	if source != restoreFunction {
		t.Fatalf("tester input selected %q, want complete function %q", source, restoreFunction)
	}
	listLabel := localHistoryText("localHistoryVersions")
	if !h.tt.Focused(previewLabel) || h.tt.Focused(listLabel) {
		t.Fatalf("preview/list focus = %v/%v, want preview-focused state", h.tt.Focused(previewLabel), h.tt.Focused(listLabel))
	}
	lineCountLabel := locales.Catalog.Format(i18n.Locale(), "localHistoryLines", map[string]any{"lines": localHistoryLineCount(historical)})
	assertLocalHistorySelectionContrast(t, h, timestampLabel, lineCountLabel, "preview focused")
	saveLocalHistoryScreenshot(t, os.Getenv("SEE_LOCAL_HISTORY_SCREENSHOT"), h)
	if err := h.tt.Click(timestampLabel); err != nil {
		t.Fatal(err)
	}
	if p.selected < 0 || p.selected >= len(p.snapshots) || p.snapshots[p.selected].Text != historical {
		t.Fatalf("focused list selected wrong snapshot: index=%d", p.selected)
	}
	if !h.tt.Focused(listLabel) || h.tt.Focused(previewLabel) {
		t.Fatalf("preview/list focus = %v/%v, want list-focused state", h.tt.Focused(previewLabel), h.tt.Focused(listLabel))
	}
	assertLocalHistorySelectionContrast(t, h, timestampLabel, lineCountLabel, "list focused")
	saveLocalHistoryScreenshot(t, os.Getenv("SEE_LOCAL_HISTORY_LIST_SCREENSHOT"), h)
	button := locales.Catalog.Format(i18n.Locale(), "localHistoryInsertAtLine", map[string]any{"line": destLine})
	if !h.tt.HasText(button) {
		t.Fatalf("restore action is not visible: %v", h.tt.Texts())
	}
	if h.tt.HasText("localHistoryLines") || h.tt.HasText("localHistorySelectionCount") {
		t.Fatalf("local history format messages are unresolved: %v", h.tt.Texts())
	}
	before := d.buf.String()
	want := before[:destStart] + source + before[destStart:]
	if err := h.tt.Click(button); err != nil {
		t.Fatal(err)
	}
	if got := d.buf.String(); got != want {
		t.Fatalf("UI restore produced %q, want %q", got, want)
	}
	if h.a.localHistoryPanel != nil {
		t.Fatal("UI restore did not close history")
	}
	waitForLocalHistory(t, store, path, func(snapshots []localhistory.Snapshot) bool {
		foundBefore, foundAfter := false, false
		for _, snapshot := range snapshots {
			foundBefore = foundBefore || snapshot.Text == before
			foundAfter = foundAfter || snapshot.Text == want
		}
		return foundBefore && foundAfter
	})
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
}

func assertLocalHistorySelectionContrast(t *testing.T, h *harness, timestampLabel, lineCountLabel, state string) {
	t.Helper()
	timestampContrast := localHistoryRenderedTextContrast(t, h, timestampLabel)
	lineCountContrast := localHistoryRenderedTextContrast(t, h, lineCountLabel)
	t.Logf("%s selected-row contrast: timestamp %.2f:1, line count %.2f:1", state, timestampContrast, lineCountContrast)
}

func localHistoryRenderedTextContrast(t *testing.T, h *harness, label string) float64 {
	t.Helper()
	box, ok := h.tt.Find(label)
	if !ok {
		t.Fatalf("rendered text %q not found", label)
	}
	img := h.tt.Image()
	x0, y0 := int(box.X), int(box.Y)
	x1, y1 := int(math.Ceil(float64(box.X+box.W))), int(math.Ceil(float64(box.Y+box.H)))
	backgroundX := x0 - 3
	backgroundY := y0 + (y1-y0)/2
	if backgroundX < 0 || backgroundY < 0 || backgroundX >= img.Bounds().Dx() || backgroundY >= img.Bounds().Dy() {
		t.Fatalf("rendered text %q has invalid box %+v", label, box)
	}
	background := img.RGBAAt(backgroundX, backgroundY)
	best, readablePixels := 0.0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			ratio := localHistoryPixelContrast(img.RGBAAt(x, y), background)
			if ratio > best {
				best = ratio
			}
			if ratio >= 4.5 {
				readablePixels++
			}
		}
	}
	if best < 4.5 || readablePixels < 5 {
		t.Fatalf("rendered text %q contrast %.2f:1 across %d pixels against %v, want at least 4.5:1", label, best, readablePixels, background)
	}
	return best
}

func localHistoryPixelContrast(a, b color.RGBA) float64 {
	luminance := func(c color.RGBA) float64 {
		channel := func(v uint8) float64 {
			s := float64(v) / 255
			if s <= 0.04045 {
				return s / 12.92
			}
			return math.Pow((s+0.055)/1.055, 2.4)
		}
		return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
	}
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func openLocalHistoryForTest(t *testing.T, h *harness, d *doc) {
	t.Helper()
	h.a.showLocalHistory(h.a.focus, d)
	if h.a.localHistoryPanel == nil {
		t.Fatal("history panel did not open")
	}
	h.settle(func() bool { return !h.a.localHistoryPanel.loading })
	if h.a.localHistoryPanel.err != nil {
		t.Fatalf("load history: %v", h.a.localHistoryPanel.err)
	}
}

func selectSnapshotText(t *testing.T, a *App, p *localHistoryPanel, text string) {
	t.Helper()
	for i, snapshot := range p.snapshots {
		if snapshot.Text == text {
			a.selectLocalHistory(p, i)
			return
		}
	}
	t.Fatalf("snapshot %q not found in %+v", text, p.snapshots)
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
