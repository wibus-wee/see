package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/workspace"
	"see/locales"
)

func TestDocumentLoadingIndicatorDelayAndCompletion(t *testing.T) {
	if documentLoadingIndicatorDelay != 800*time.Millisecond {
		t.Fatalf("document loading indicator delay = %v, want 800ms", documentLoadingIndicatorDelay)
	}
	h, d := newLoadingDoc(t, "pending.go")
	defer stopLoadingIndicator(d)

	h.tt.Frame()
	baseProgress := loadingProgressCount(h.tt)
	h.a.scheduleLoadingIndicator(d, d.seq, documentLoadingIndicatorDelay)
	time.Sleep(100 * time.Millisecond)
	h.tt.Frame()
	if h.tt.HasText(loadingText(d)) {
		t.Fatal("loading label appeared before its delay")
	}
	if got := loadingProgressCount(h.tt); got != baseProgress {
		t.Fatalf("progress indicators appeared before their delay: %d, want %d", got, baseProgress)
	}
	waitForLoadingIndicator(t, h, d)
	h.tt.Frame()
	if !h.tt.HasText(loadingText(d)) {
		t.Fatalf("delayed loading label missing: %q", h.tt.Texts())
	}
	if got := loadingProgressCount(h.tt); got != baseProgress+1 {
		t.Fatalf("visible full-pane progress indicators = %d, want %d", got, baseProgress+1)
	}

	text := "package main\n// ready\n"
	h.a.loaded(d, workspace.File{Kind: workspace.Text, Text: text}, editor.NewBuffer(text), false, nil)
	h.tt.Frame()
	if d.loading || d.loadingVisible || h.tt.HasText(loadingText(d)) {
		t.Fatalf("loading indicator remained after content was ready: loading=%v visible=%v texts=%q", d.loading, d.loadingVisible, h.tt.Texts())
	}
	if got := loadingProgressCount(h.tt); got != baseProgress {
		t.Fatalf("progress indicators after load = %d, want %d", got, baseProgress)
	}
	if d.buf == nil || h.a.focus.eds[d.path] == nil {
		t.Fatal("ready content was not installed immediately")
	}
}

func TestFastDocumentLoadDoesNotShowDelayedIndicator(t *testing.T) {
	h, d := newLoadingDoc(t, "fast.go")
	defer stopLoadingIndicator(d)

	h.tt.Frame()
	baseProgress := loadingProgressCount(h.tt)
	h.a.loadDoc(d)
	h.tt.Frame()
	if d.loadingVisible || h.tt.HasText(loadingText(d)) {
		t.Fatalf("fast load showed an early loading indicator: visible=%v texts=%q", d.loadingVisible, h.tt.Texts())
	}
	h.settle(func() bool { return !d.loading })
	if d.loading || d.loadingVisible || h.tt.HasText(loadingText(d)) {
		t.Fatalf("fast load showed or retained a loading indicator: loading=%v visible=%v texts=%q", d.loading, d.loadingVisible, h.tt.Texts())
	}
	if got := loadingProgressCount(h.tt); got != baseProgress {
		t.Fatalf("progress indicators after fast load = %d, want %d", got, baseProgress)
	}
	if d.buf == nil || h.a.focus.eds[d.path] == nil {
		t.Fatal("fast-loaded content was not installed")
	}
}

func TestClosingDocumentCancelsDelayedLoadingIndicator(t *testing.T) {
	h, d := newLoadingDoc(t, "closed.go")
	defer stopLoadingIndicator(d)
	h.a.scheduleLoadingIndicator(d, d.seq, 40*time.Millisecond)
	h.a.closeTab(h.a.focus, d.path)
	time.Sleep(60 * time.Millisecond)
	h.tt.Frame()
	if h.a.docs[d.path] != nil || d.loadingVisible {
		t.Fatalf("closed document's loading indicator survived: document=%v visible=%v", h.a.docs[d.path], d.loadingVisible)
	}
	if h.tt.HasText(loadingText(d)) {
		t.Fatalf("closed document still has a loading label: %q", h.tt.Texts())
	}
}

func newLoadingDoc(t *testing.T, name string) (*harness, *doc) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte("package test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	path := filepath.Join(h.a.root, name)
	d := &doc{path: path, uri: uriOf(path), name: name, loading: true, seq: 1}
	h.a.docs[path] = d
	g := h.a.focus
	g.addTab(path, 0)
	h.a.activateIn(g, path)
	return h, d
}

func waitForLoadingIndicator(t *testing.T, h *harness, d *doc) {
	t.Helper()
	guard := time.NewTimer(2 * time.Second)
	defer guard.Stop()
	for !d.loadingVisible {
		select {
		case fn := <-h.q:
			fn()
		case <-guard.C:
			t.Fatal("delayed loading indicator was not posted")
		}
	}
}

func loadingProgressCount(tt *ui.Tester) int {
	count := 0
	for _, access := range tt.AccessAll() {
		if access.Role == ui.RoleProgress {
			count++
		}
	}
	return count
}

func loadingText(d *doc) string { return locales.MsgLoading2() + d.name + "…" }
