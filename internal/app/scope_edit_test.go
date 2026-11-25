package app

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/locales"
)

const scopeEditFixture = "package demo\n\nfunc render() {\n\t名字 := \"🌿\"\n\tprintln(\"🎉\", 名字)\n\t{\n\t\t名字 := \"shadow\"\n\t\tprintln(名字)\n\t}\n\tprintln(\"🎉\", 名字)\n}\n"

type scopeHighlightClient struct {
	*fakeClient
	supported  bool
	answer     func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error)
	snapshotMu sync.Mutex
	snapshot   string
}

func (c *scopeHighlightClient) HasDocumentHighlights() bool { return c.supported }

func (c *scopeHighlightClient) DidOpen(uri, language string, version int, text string) error {
	c.snapshotMu.Lock()
	c.snapshot = text
	c.snapshotMu.Unlock()
	return c.fakeClient.DidOpen(uri, language, version, text)
}

func (c *scopeHighlightClient) DidChange(uri string, version int, text string) error {
	c.snapshotMu.Lock()
	c.snapshot = text
	c.snapshotMu.Unlock()
	return c.fakeClient.DidChange(uri, version, text)
}

func (c *scopeHighlightClient) synchronizedText() string {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	return c.snapshot
}

func (c *scopeHighlightClient) DocumentHighlights(ctx context.Context, uri string, pos lsp.Position) ([]lsp.DocumentHighlight, error) {
	c.record("documentHighlight")
	return c.answer(ctx, uri, pos)
}

func newScopeHarness(t *testing.T, answer func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error)) (*harness, *doc, *scopeHighlightClient) {
	return newScopeHarnessWithEncoding(t, answer, lsp.UTF16)
}

func newScopeHarnessWithEncoding(t *testing.T, answer func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error), encoding string) (*harness, *doc, *scopeHighlightClient) {
	return newScopeHarnessWithSource(t, scopeEditFixture, answer, encoding)
}

func newScopeHarnessWithSource(t *testing.T, source string, answer func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error), encoding string) (*harness, *doc, *scopeHighlightClient) {
	t.Helper()
	root := writeTree(t, map[string]string{"main.go": source, "other.go": "package demo\nfunc other() {}\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	client := &scopeHighlightClient{
		fakeClient: &fakeClient{enc: encoding},
		supported:  true,
		answer:     answer,
	}
	manager := serverOf(root, client)
	h.a.lsp = manager
	t.Cleanup(func() { manager.StopWait(5 * time.Second) })
	return h, h.open("main.go"), client
}

func scopeOffsets(text, name string) []int {
	var out []int
	for at := 0; ; {
		i := strings.Index(text[at:], name)
		if i < 0 {
			return out
		}
		at += i
		out = append(out, at)
		at += len(name)
	}
}

func lspPositionAt(text string, offset int, encoding string) lsp.Position {
	prefix := text[:offset]
	line := strings.Count(prefix, "\n")
	lineStart := strings.LastIndex(prefix, "\n") + 1
	lineEnd := strings.IndexByte(text[lineStart:], '\n')
	if lineEnd < 0 {
		lineEnd = len(text)
	} else {
		lineEnd += lineStart
	}
	return lsp.Position{Line: line, Character: lsp.ColumnToLSP(text[lineStart:lineEnd], offset-lineStart, encoding)}
}

func scopeHighlights(text string, offsets []int, name, encoding string) []lsp.DocumentHighlight {
	out := make([]lsp.DocumentHighlight, 0, len(offsets))
	for _, offset := range offsets {
		out = append(out, lsp.DocumentHighlight{Range: lsp.Range{
			Start: lspPositionAt(text, offset, encoding),
			End:   lspPositionAt(text, offset+len(name), encoding),
		}})
	}
	return out
}

func TestEditAllInScopeShortcutAndPalette(t *testing.T) {
	for _, palette := range []bool{false, true} {
		name := "shortcut"
		if palette {
			name = "command palette"
		}
		for _, encoding := range []string{lsp.UTF8, lsp.UTF16} {
			t.Run(name+" "+encoding, func(t *testing.T) {
				offsets := scopeOffsets(scopeEditFixture, "名字")
				outer := []int{offsets[0], offsets[1], offsets[4]}
				wantPosition := lspPositionAt(scopeEditFixture, offsets[1], encoding)
				h, d, client := newScopeHarnessWithEncoding(t, func(_ context.Context, _ string, pos lsp.Position) ([]lsp.DocumentHighlight, error) {
					if pos != wantPosition {
						t.Errorf("request position = %+v, want %+v", pos, wantPosition)
					}
					ranges := scopeHighlights(scopeEditFixture, outer, "名字", encoding)
					return []lsp.DocumentHighlight{ranges[2], ranges[0], ranges[1], ranges[0]}, nil
				}, encoding)
				st := h.a.activeEditor()
				st.GoTo(d.buf.Pos(offsets[1]))
				h.tt.Frame()
				if !h.a.commandEnabled("edit.allInScope") {
					t.Fatal("Edit All in Scope is disabled for a Go source file")
				}
				if palette {
					h.a.openPalette()
					h.a.quick.query = ">Edit All in Scope"
					h.a.rankQuick()
					h.tt.Frame()
					h.tt.Key(0, ui.KeyEnter)
				} else {
					_, _, mods, key := editAllScopeShortcut(runtime.GOOS)
					h.tt.Key(mods, key)
				}
				h.settle(func() bool { return len(st.Selections()) == 3 })
				want := []editor.Selection{
					{Anchor: outer[0], Head: outer[0] + len("名字")},
					{Anchor: outer[1], Head: outer[1] + len("名字")},
					{Anchor: outer[2], Head: outer[2] + len("名字")},
				}
				if !slices.Equal(st.Selections(), want) {
					t.Fatalf("selections = %+v, want %+v", st.Selections(), want)
				}
				if primary := st.Selection(); primary.Anchor != outer[1] || primary.Head != outer[1]+len("名字") {
					t.Fatalf("primary selection = %+v, want occurrence at %d", primary, outer[1])
				}
				if !h.tt.HasText("Esc to finish") {
					t.Fatalf("missing active-mode hint: %v", h.tt.Texts())
				}
				if slices.Contains(client.calls(), "documentHighlight") == false {
					t.Fatal("document highlight request was not sent")
				}
				shadow := offsets[2]
				for _, selection := range st.Selections() {
					if selection.Start() == shadow {
						t.Fatal("shadowed identifier was selected")
					}
				}
				if !slices.Contains(client.calls(), "open main.go") {
					t.Fatal("buffer snapshot was not synchronized before the request")
				}
			})
		}
	}
}

func TestEditAllInScopeAfterClosingBytesPreview(t *testing.T) {
	offsets := scopeOffsets(scopeEditFixture, "名字")
	outer := []int{offsets[0], offsets[1], offsets[4]}
	h, d, _ := newScopeHarness(t, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
		return scopeHighlights(scopeEditFixture, outer, "名字", lsp.UTF16), nil
	})
	g, st := h.a.focus, h.a.activeEditor()
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()

	if err := h.tt.Click(locales.MsgFileBytes()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !g.hexPreview[d.path] || d.hex == nil {
		t.Fatal("byte preview did not open and populate its cache")
	}
	if h.a.commandEnabled("edit.allInScope") {
		t.Fatal("Edit All in Scope should be disabled while the byte preview is active")
	}

	if err := h.tt.Click(locales.MsgFileText()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if g.hexPreview[d.path] || d.hex == nil {
		t.Fatal("closing the byte preview should retain only its cached state")
	}
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	if !h.a.commandEnabled("edit.allInScope") {
		t.Fatal("cached byte-preview state disabled Edit All in Scope after returning to source")
	}

	h.a.openPalette()
	h.a.quick.query = ">Edit All in Scope"
	h.a.rankQuick()
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	h.settle(func() bool { return len(st.Selections()) == 3 || h.a.notice != "" })
	if len(st.Selections()) != 3 {
		t.Fatalf("Edit All in Scope did not select the source occurrences after closing the byte preview: selections=%+v notice=%q", st.Selections(), h.a.notice)
	}
}

func TestEditAllInScopeEditorContextMenuTargetsClickedPane(t *testing.T) {
	offsets := scopeOffsets(scopeEditFixture, "名字")
	outer := []int{offsets[0], offsets[1], offsets[4]}
	h, d, _ := newScopeHarness(t, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
		return scopeHighlights(scopeEditFixture, outer, "名字", lsp.UTF16), nil
	})
	target := h.a.focus
	targetState := target.eds[d.path]
	targetState.GoTo(d.buf.Pos(offsets[1]))
	h.a.splitActive(sideRight)
	active := h.a.focus
	activeState := active.eds[d.path]
	activeState.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()

	if target == active {
		t.Fatal("split did not create a separate editor group")
	}
	// A real pointer moves before it presses, and the first move settles
	// the element tree's pointer-in transition; pressing without a prior
	// move leaves the asked context menu's element id stale for a frame.
	h.tt.Move(400, 140)
	h.tt.RightClickAt(400, 140)
	menu := h.tt.Menu()
	if len(menu) == 0 {
		t.Fatal("right-click did not open the editor context menu")
	}
	for _, item := range []string{"Cut", "Copy", "Paste", "Find References", commandLabel("edit.allInScope")} {
		if !slices.Contains(menu, item) {
			t.Fatalf("context menu is missing %q: %v", item, menu)
		}
	}

	h.a.focus = active
	targetState.GoTo(d.buf.Pos(offsets[1]))
	activeState.GoTo(d.buf.Pos(offsets[1]))
	if err := h.tt.ChooseMenuItem(commandLabel("edit.allInScope")); err != nil {
		t.Fatalf("%v; menu=%v", err, h.tt.Menu())
	}
	h.settle(func() bool { return len(targetState.Selections()) == 3 || h.a.notice != "" })
	if len(targetState.Selections()) != 3 || len(activeState.Selections()) != 1 {
		t.Fatalf("context command targeted the active editor: clicked=%+v active=%+v notice=%q", targetState.Selections(), activeState.Selections(), h.a.notice)
	}
	if h.a.scopeModeState != targetState || h.a.scopeModeGroup != target {
		t.Fatal("scope mode did not retain the clicked editor target")
	}
}

func TestEditAllInScopeSynchronizesLatestSnapshot(t *testing.T) {
	updated := scopeEditFixture + "\n// unsynchronized edit\n"
	offsets := scopeOffsets(updated, "名字")
	outer := []int{offsets[0], offsets[1], offsets[4]}
	var client *scopeHighlightClient
	h, d, client := newScopeHarness(t, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
		if got := client.synchronizedText(); got != updated {
			t.Errorf("server snapshot = %q, want %q", got, updated)
		}
		return scopeHighlights(updated, outer, "名字", lsp.UTF16), nil
	})
	d.buf.Set(updated)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offsets[1]))
	h.a.editAllInScope()
	h.settle(func() bool { return len(st.Selections()) == 3 || h.a.notice != "" })
	if got := client.synchronizedText(); got != updated || len(st.Selections()) != 3 {
		t.Fatalf("latest snapshot was not used: synchronized=%q selections=%+v notice=%q", got, st.Selections(), h.a.notice)
	}
}

func TestEditAllInScopeLinkedTypingUndoAndEscape(t *testing.T) {
	offsets := scopeOffsets(scopeEditFixture, "名字")
	outer := []int{offsets[0], offsets[1], offsets[4]}
	h, d, _ := newScopeHarness(t, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
		return scopeHighlights(scopeEditFixture, outer, "名字", lsp.UTF16), nil
	})
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	h.a.editAllInScope()
	h.settle(func() bool { return len(st.Selections()) == 3 })
	h.tt.Type("renamed")
	h.tt.Frame()
	want := scopeEditFixture
	for i := len(outer) - 1; i >= 0; i-- {
		offset := outer[i]
		want = want[:offset] + "renamed" + want[offset+len("名字"):]
	}
	if got := d.buf.String(); got != want {
		t.Fatalf("linked typing produced:\n%s\nwant:\n%s", got, want)
	}
	if len(st.Selections()) != 3 {
		t.Fatalf("typing lost multicursors: %+v", st.Selections())
	}
	h.tt.Key(ui.Cmd, ui.KeyZ)
	h.tt.Frame()
	if got := d.buf.String(); got != scopeEditFixture {
		t.Fatalf("undo produced:\n%s\nwant:\n%s", got, scopeEditFixture)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if len(st.Selections()) != 1 {
		t.Fatalf("Escape left selections active: %+v", st.Selections())
	}
	if h.tt.HasText("Esc to finish") {
		t.Fatal("active-mode hint remained after Escape")
	}
}

func TestEditAllInScopeModeSurvivesFocusChange(t *testing.T) {
	offsets := scopeOffsets(scopeEditFixture, "名字")
	outer := []int{offsets[0], offsets[1], offsets[4]}
	h, d, _ := newScopeHarness(t, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
		return scopeHighlights(scopeEditFixture, outer, "名字", lsp.UTF16), nil
	})
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	h.a.editAllInScope()
	h.settle(func() bool { return len(st.Selections()) == 3 })
	scopeGroup := h.a.focus
	h.a.focus = &group{}
	if status := h.a.scopeEditStatus(st); status != "" {
		t.Fatalf("inactive editor showed scope status %q", status)
	}
	if h.a.scopeModeState != st {
		t.Fatal("focus change cleared the still-active scope selections")
	}
	h.a.focus = scopeGroup
	if status := h.a.scopeEditStatus(st); !strings.Contains(status, "Esc to finish") {
		t.Fatalf("scope mode hint did not return with the selected editor: %q", status)
	}
}

func TestEditAllInScopeNoticesDoNotChangeSelections(t *testing.T) {
	for _, tc := range []struct {
		name      string
		supported bool
		answer    func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error)
		want      string
	}{
		{"unsupported", false, nil, scopeEditText("scopeEditUnsupported", nil)},
		{"empty", true, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) { return nil, nil }, scopeEditText("scopeEditEmpty", nil)},
		{"error", true, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
			return nil, errors.New("server failed")
		}, "server failed"},
		{"invalid range", true, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
			return []lsp.DocumentHighlight{{Range: lsp.Range{Start: lsp.Position{Line: 99}, End: lsp.Position{Line: 99, Character: 1}}}}, nil
		}, scopeEditText("scopeEditFailed", map[string]any{"error": "invalid start position"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer := tc.answer
			if answer == nil {
				answer = func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
					return nil, nil
				}
			}
			h, _, client := newScopeHarness(t, answer)
			client.supported = tc.supported
			offset := scopeOffsets(scopeEditFixture, "名字")[1]
			st := h.a.activeEditor()
			st.Select(offset, offset+len("名字"))
			h.tt.Frame()
			before := slices.Clone(st.Selections())
			h.a.editAllInScope()
			h.settle(func() bool { return h.a.notice != "" })
			if got := st.Selections(); !slices.Equal(got, before) {
				t.Fatalf("failure changed selections: %+v, before %+v", got, before)
			}
			if !strings.Contains(h.a.notice, tc.want) {
				t.Fatalf("notice %q does not contain %q", h.a.notice, tc.want)
			}
		})
	}
	t.Run("missing optional provider", func(t *testing.T) {
		h := newHarness(t, writeTree(t, map[string]string{"main.go": scopeEditFixture}))
		h.a.cfg.Trust(h.a.root)
		manager := serverOf(h.a.root, h.fc)
		h.a.lsp = manager
		t.Cleanup(func() { manager.StopWait(5 * time.Second) })
		h.open("main.go")
		offset := scopeOffsets(scopeEditFixture, "名字")[1]
		st := h.a.activeEditor()
		st.Select(offset, offset+len("名字"))
		before := slices.Clone(st.Selections())
		h.a.editAllInScope()
		h.settle(func() bool { return h.a.notice != "" })
		if !strings.Contains(h.a.notice, scopeEditText("scopeEditUnsupported", nil)) || !slices.Equal(st.Selections(), before) {
			t.Fatalf("unsupported provider: notice=%q selections=%+v", h.a.notice, st.Selections())
		}
	})
}

func TestScopeSelectionsValidateAndDeduplicate(t *testing.T) {
	text := "🎉 名字 名字"
	snap := editor.NewBuffer(text).Snapshot()
	name := "名字"
	offset := strings.Index(text, name)
	first := scopeHighlights(text, []int{offset}, name, lsp.UTF16)[0]
	secondOffset := strings.LastIndex(text, name)
	second := scopeHighlights(text, []int{secondOffset}, name, lsp.UTF16)[0]
	got, err := scopeSelections(snap, []lsp.DocumentHighlight{second, first, first}, name, offset, lsp.UTF16)
	if err != nil {
		t.Fatal(err)
	}
	want := []editor.Selection{{Anchor: secondOffset, Head: secondOffset + len(name)}, {Anchor: offset, Head: offset + len(name)}}
	if !slices.Equal(got, want) {
		t.Fatalf("selections = %+v, want primary last: %+v", got, want)
	}
	if selections, err := scopeSelections(snap, nil, name, offset, lsp.UTF16); err != nil || len(selections) != 0 {
		t.Fatalf("empty highlights = %+v, %v", selections, err)
	}

	midSurrogate := first
	midSurrogate.Range.Start.Character = 1
	beyondLine := first
	beyondLine.Range.Start.Character = 99
	mismatchText := "xyz"
	mismatchSnap := editor.NewBuffer(mismatchText).Snapshot()
	nonIdentifier := scopeHighlights(mismatchText, []int{0}, "abc", lsp.UTF16)[0]
	noCursor := scopeHighlights(text, []int{secondOffset}, name, lsp.UTF16)
	overlapText := "aaaaa"
	overlapSnap := editor.NewBuffer(overlapText).Snapshot()
	overlap := scopeHighlights(overlapText, []int{0, 2}, "aaa", lsp.UTF16)
	for _, tc := range []struct {
		name       string
		snapshot   editor.Snapshot
		highlights []lsp.DocumentHighlight
		target     string
		cursor     int
		encoding   string
	}{
		{"mid surrogate", snap, []lsp.DocumentHighlight{midSurrogate}, name, offset, lsp.UTF16},
		{"beyond line", snap, []lsp.DocumentHighlight{beyondLine}, name, offset, lsp.UTF16},
		{"different identifier", mismatchSnap, []lsp.DocumentHighlight{nonIdentifier}, "abc", 0, lsp.UTF16},
		{"cursor not covered", snap, noCursor, name, offset, lsp.UTF16},
		{"overlap", overlapSnap, overlap, "aaa", 2, lsp.UTF16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := scopeSelections(tc.snapshot, tc.highlights, tc.target, tc.cursor, tc.encoding); err == nil {
				t.Fatal("invalid highlight result was accepted")
			}
		})
	}
}

func TestEditAllInScopeRejectsStaleResults(t *testing.T) {
	for _, scenario := range []string{"workspace", "document", "version", "editor", "selection", "server"} {
		t.Run(scenario, func(t *testing.T) {
			offsets := scopeOffsets(scopeEditFixture, "名字")
			outer := []int{offsets[0], offsets[1], offsets[4]}
			entered, release := make(chan struct{}), make(chan struct{})
			h, d, _ := newScopeHarness(t, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
				close(entered)
				<-release
				return scopeHighlights(scopeEditFixture, outer, "名字", lsp.UTF16), nil
			})
			st := h.a.activeEditor()
			st.GoTo(d.buf.Pos(offsets[1]))
			h.tt.Frame()
			h.a.editAllInScope()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("highlight request did not start")
			}
			switch scenario {
			case "workspace":
				h.a.openWorkspace(writeTree(t, map[string]string{"next.go": "package next\n"}))
				h.tt.Frame()
			case "document":
				h.a.closeDoc(d.path)
				h.open("main.go")
			case "version":
				d.buf.Set(d.buf.String() + "// changed\n")
				h.tt.Frame()
			case "editor":
				h.open("other.go")
			case "selection":
				st.GoTo(d.buf.Pos(offsets[1] + len("名")))
				h.tt.Frame()
			case "server":
				manager := serverOf(h.a.root, &fakeClient{enc: lsp.UTF16})
				h.a.lsp = manager
				t.Cleanup(func() { manager.StopWait(5 * time.Second) })
			}
			current := h.a.activeEditor()
			var before []editor.Selection
			if current != nil {
				before = slices.Clone(current.Selections())
			}
			close(release)
			h.settle(func() bool { return h.a.scopeCancel == nil })
			if current != nil && !slices.Equal(current.Selections(), before) {
				t.Fatalf("stale request changed selections: %+v, before %+v", current.Selections(), before)
			}
			if h.a.notice != "" {
				t.Fatalf("stale request produced a notice: %q", h.a.notice)
			}
		})
	}
}

func TestEditAllInScopeNewRequestSupersedesPrevious(t *testing.T) {
	offsets := scopeOffsets(scopeEditFixture, "名字")
	outer := []int{offsets[0], offsets[1], offsets[4]}
	entered, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	h, d, _ := newScopeHarness(t, func(ctx context.Context, _ string, _ lsp.Position) ([]lsp.DocumentHighlight, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}
		return scopeHighlights(scopeEditFixture, outer, "名字", lsp.UTF16), nil
	})
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	h.a.editAllInScope()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}
	h.a.editAllInScope()
	h.settle(func() bool { return len(st.Selections()) == 3 })
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("superseded request was not canceled")
	}
	if len(st.Selections()) != 3 || h.a.notice != "" {
		t.Fatalf("new request did not win: selections=%+v notice=%q", st.Selections(), h.a.notice)
	}
}

func TestEditAllInScopeEscapeCancelsPendingRequest(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan struct{})
	h, d, _ := newScopeHarness(t, func(ctx context.Context, _ string, _ lsp.Position) ([]lsp.DocumentHighlight, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	offset := scopeOffsets(scopeEditFixture, "名字")[1]
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset))
	h.tt.Frame()
	before := slices.Clone(st.Selections())
	h.a.editAllInScope()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("highlight request did not start")
	}
	h.tt.Key(0, ui.KeyEscape)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("Escape did not cancel the request context")
	}
	if !slices.Equal(st.Selections(), before) || h.a.notice != "" {
		t.Fatalf("cancel changed editor state: selections=%+v notice=%q", st.Selections(), h.a.notice)
	}
}

func TestEditAllScopeShortcutDoesNotConflict(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		label, keyName, mods, key := editAllScopeShortcut(goos)
		wantLabel := "Ctrl+Shift+"
		if goos == "darwin" {
			wantLabel = "Ctrl+Cmd+"
		}
		if label != wantLabel {
			t.Errorf("%s shortcut label = %q, want %q", goos, label, wantLabel)
		}
		if keyName != "E" || key != ui.KeyE {
			t.Fatalf("%s shortcut key = %q/%v", goos, keyName, key)
		}
		want := ui.Ctrl | ui.Shift
		if goos == "darwin" {
			want = ui.Ctrl | ui.Super
		}
		if mods != want {
			t.Errorf("%s modifiers = %b, want effective modifiers %b", goos, mods, want)
		}
		var scopeCommand command
		commands := commandsFor(goos)
		for _, cmd := range commands {
			if cmd.name == "Edit All in Scope" {
				scopeCommand = cmd
				break
			}
		}
		if scopeCommand.shortcutKey != key || scopeCommand.shortcutMods != want {
			t.Errorf("%s effective command binding = %v/%b, want %v/%b", goos, scopeCommand.shortcutKey, scopeCommand.shortcutMods, key, want)
		}
		for _, cmd := range commands {
			if cmd.name != "Edit All in Scope" && cmd.shortcutKey == key && cmd.shortcutMods == mods {
				t.Errorf("shortcut conflicts with command %q", cmd.name)
			}
		}
		if goos == runtime.GOOS {
			for _, binding := range editor.DefaultBindings() {
				if binding.Key == key && binding.Mods == mods {
					t.Errorf("shortcut conflicts with MyGo editor command %v", binding.Command)
				}
			}
			for _, binding := range editorBindings() {
				if binding.Key == key && binding.Mods == mods && binding.Command != editor.NoCommand {
					t.Errorf("shortcut is claimed by app editor binding %v", binding.Command)
				}
			}
		}
	}
}

func TestEditAllInScopeTrustAndSourceGating(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go":   scopeEditFixture,
		"README.md": "# Notes\n",
	})
	h := newHarness(t, root)
	manager := serverOf(root, h.fc)
	h.a.lsp = manager
	t.Cleanup(func() { manager.StopWait(5 * time.Second) })
	d := h.open("main.go")
	offset := scopeOffsets(scopeEditFixture, "名字")[1]
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(offset))
	h.tt.Frame()
	before := slices.Clone(st.Selections())
	current := manager.Current()
	h.a.editAllInScope()
	if !h.a.trustAsk || h.a.scopeCancel != nil || manager.Current() != current || !slices.Equal(st.Selections(), before) {
		t.Fatalf("untrusted workspace started or changed an edit: trustAsk=%v client=%v selections=%+v", h.a.trustAsk, manager.Current(), st.Selections())
	}
	st.ReadOnly = true
	if h.a.commandEnabled("edit.allInScope") {
		t.Fatal("command enabled for a read-only editor")
	}
	st.ReadOnly = false
	h.open("README.md")
	if h.a.commandEnabled("edit.allInScope") {
		t.Fatal("command enabled for a Markdown document")
	}
}

func TestEditAllInScopeWithoutLanguageServerDoesNotMutate(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"main.go": scopeEditFixture}))
	h.a.lsp = nil
	h.open("main.go")
	offset := scopeOffsets(scopeEditFixture, "名字")[1]
	st := h.a.activeEditor()
	st.Select(offset, offset+len("名字"))
	before := slices.Clone(st.Selections())
	if h.a.commandEnabled("edit.allInScope") {
		t.Fatal("command enabled without a language server")
	}
	h.a.editAllInScope()
	if h.a.notice == "" || !slices.Equal(st.Selections(), before) {
		t.Fatalf("missing-server request changed selections or gave no notice: notice=%q selections=%+v", h.a.notice, st.Selections())
	}
}

func TestEditAllInScopeScreenshots(t *testing.T) {
	dir := os.Getenv("SEE_SCOPE_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("set SEE_SCOPE_SCREENSHOT_DIR to write screenshots")
	}
	source := "package demo\n\nfunc apiURL(userID string) string {\n\tendpoint := \"/api/v1/users/\" + userID\n\tprintln(endpoint)\n\t{\n\t\tendpoint := \"/internal/admin\"\n\t\tprintln(endpoint)\n\t}\n\treturn endpoint\n}\n"
	offsets := scopeOffsets(source, "endpoint")
	outer := []int{offsets[0], offsets[1], offsets[4]}
	h, d, _ := newScopeHarnessWithSource(t, source, func(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error) {
		return scopeHighlights(source, outer, "endpoint", lsp.UTF16), nil
	}, lsp.UTF16)
	h.a.activeEditor().GoTo(d.buf.Pos(offsets[1]))
	h.tt.Frame()
	h.a.editAllInScope()
	h.settle(func() bool { return h.a.notice != "" || len(h.a.activeEditor().Selections()) == 3 })
	if h.a.notice != "" {
		t.Fatalf("scope selection failed before screenshot: %q", h.a.notice)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, theme := range []struct {
		name string
		dark bool
	}{{"light", false}, {"dark", true}} {
		h.tt.SetDark(theme.dark)
		h.tt.Frame()
		path := filepath.Join(dir, "edit-all-scope-"+theme.name+".png")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, h.tt.Image())
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		t.Logf("Screenshot: %s", path)
	}
}
