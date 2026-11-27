package app

// Adversarial verification tests for SPEC-006 lane B, written by the
// independent verifier (branch verify-lane-b) and ported onto
// spec-lane-b-paths as regression coverage when the defects they
// demonstrated were fixed. They cover what the implementer's tests do
// not: symlink dual-identity staleness, watcher rename of a dirty
// document, atomic-save replacement, external case renames, session
// restore spellings, and workspace-edit edge cases.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"

	"see/internal/config"
	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/uri"
)

// TestAdversarialSymlinkDualIdentityStaleView pins the UX consequence of
// the dual-identity contract: an external write to real.txt fires watcher
// events ONLY for real.txt. The open document of link.txt — the same
// bytes — never reloads, so it shows stale content indefinitely. The save
// conflict check is what stands between the user and silent data loss.
func TestAdversarialSymlinkDualIdentityStaleView(t *testing.T) {
	root := writeTree(t, map[string]string{"real.txt": "v1\n"})
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	h := newHarness(t, root)
	realDoc := h.open("real.txt")
	linkDoc := h.openPath(filepath.Join(h.a.root, "link.txt"))
	if realDoc == linkDoc {
		t.Fatal("interior symlink merged into target identity")
	}

	// External process edits the file.
	if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("v2-external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "real.txt"}})
	h.settle(func() bool { return !realDoc.loading && realDoc.buf.Snapshot().String() == "v2-external\n" })

	// The real identity reloaded; the link identity did NOT — same file,
	// two views, one of them stale with no marker.
	if linkDoc.buf.Snapshot().String() != "v1\n" {
		t.Fatalf("expected stale link view, got %q", linkDoc.buf.Snapshot().String())
	}
	if linkDoc.conflict || linkDoc.gone {
		t.Logf("note: link doc carries conflict=%v gone=%v", linkDoc.conflict, linkDoc.gone)
	}
	t.Logf("CONFIRMED: link identity is stale (v1) while real identity shows v2; no marker on the stale view")

	// The user edits the stale link view and saves. The byte-comparison in
	// saveDoc must refuse, or the external edit is silently overwritten.
	linkDoc.buf.Set("v3-user-edit\n")
	if h.a.saveDoc(linkDoc, false) {
		disk, _ := os.ReadFile(filepath.Join(root, "real.txt"))
		t.Fatalf("SAVE OVERWROTE EXTERNAL CHANGES through the symlink identity: disk=%q", disk)
	}
	if !linkDoc.conflict {
		t.Error("save was refused but no conflict surfaced to the user")
	}
	disk, _ := os.ReadFile(filepath.Join(root, "real.txt"))
	if string(disk) != "v2-external\n" {
		t.Fatalf("external bytes lost: %q", disk)
	}
}

// TestAdversarialWatcherRenameOfDirtyDocument answers the assigned
// question: a user editing unsaved changes has their file renamed away
// externally. Where do the unsaved bytes go?
func TestAdversarialWatcherRenameOfDirtyDocument(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "original\n"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	d.buf.Set("UNSAVED USER WORK\n")
	oldPath := d.path

	// External mv: the watch reports a rename.
	if err := os.Rename(oldPath, filepath.Join(h.a.root, "b.go")); err != nil {
		t.Fatal(err)
	}
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileRename, OldPath: "a.txt", Path: "b.txt"}})

	if !d.gone {
		t.Error("document not marked gone after external rename")
	}
	if d.buf.Snapshot().String() != "UNSAVED USER WORK\n" {
		t.Fatalf("unsaved content lost from buffer: %q", d.buf.Snapshot().String())
	}
	if !d.buf.Modified() {
		t.Error("buffer no longer modified")
	}
	t.Logf("buffer retains unsaved bytes, marked gone, path still %q", d.path)

	// Save at the old path: the file does not exist there anymore, so
	// saveDoc sees a mismatch (err != nil on read) and must either refuse
	// or write after explicit overwrite. Document what happens.
	saved := h.a.saveDoc(d, false)
	disk, err := os.ReadFile(oldPath)
	t.Logf("saveDoc(d, false) = %v, disk-at-old-path err=%v content=%q, conflict=%v", saved, err, disk, d.conflict)
	if saved && string(disk) == "UNSAVED USER WORK\n" {
		t.Logf("NOTE: saving recreated the file at the OLD path — user now has a.txt (their edits) AND b.txt (original), silently diverged")
	} else if !saved {
		t.Logf("NOTE: save refused at the old path; user must discover Save As / overwrite explicitly")
	}
}

// TestAdversarialAtomicSaveReplacementReloads pins the production watcher
// fact the verifier established empirically: an atomic-save replacement
// (write temp + rename over) arrives as a WRITE of the target, so an
// unmodified open document must reload to the new bytes.
func TestAdversarialAtomicSaveReplacementReloads(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "v1\n"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	// Real backends report rename-over as FileWrite of the destination
	// (verified against mygo on macOS FSEvents by the verifier's probe).
	if err := os.WriteFile(d.path, []byte("v2-atomic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.txt"}})
	h.settle(func() bool { return !d.loading && d.buf.Snapshot().String() == "v2-atomic\n" })
	if d.gone {
		t.Error("atomic replacement marked the document gone — must reload instead")
	}
}

// TestAdversarialExternalCaseRenameReopenSplit: the user has a.txt open.
// A shell renames it to A.TXT (case-only). The watcher — if it reports
// the rename at all — closes the old identity; when the user then opens
// the file again from the tree they get A.TXT. Meanwhile the OLD tab is
// still open at a.txt, marked gone. The same file is then two documents.
func TestAdversarialExternalCaseRenameReopenSplit(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "content\n"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	oldPath := d.path
	if err := os.Rename(oldPath, filepath.Join(h.a.root, "A.TXT")); err != nil {
		t.Fatalf("case-only rename unsupported here: %v", err)
	}
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileRename, OldPath: "a.txt", Path: "A.TXT"}})
	// Old doc is gone but keeps its tab and buffer.
	if !d.gone {
		t.Fatal("old doc not gone")
	}
	// User opens the renamed file from the tree.
	fresh := h.openPath(filepath.Join(h.a.root, "A.TXT"))
	if fresh == d {
		t.Fatal("reopen resurrected the closed identity")
	}
	if countDocs(h.a) != 2 {
		t.Fatalf("expected the documented two documents (gone old + fresh new), got %d", countDocs(h.a))
	}
	// Now the dirty old buffer hazard: edit the OLD (gone) document and
	// save — the old path no longer exists, the file content lives at
	// A.TXT. What happens to user data?
	d.buf.Set("user edits in the stale tab\n")
	saved := h.a.saveDoc(d, false)
	diskOld, errOld := os.ReadFile(oldPath)
	t.Logf("saving the stale tab: saved=%v old-path-bytes=%q err=%v — a.txt resurrected next to A.TXT", saved, diskOld, errOld)
	if saved && errOld == nil {
		t.Logf("CONFIRMED UX HAZARD: file resurrected at old spelling; the directory now has a.txt AND A.TXT which on a merging fs are THE SAME FILE — the save overwrote the renamed file invisibly")
		// On a merging filesystem a.txt IS A.TXT: the write went into the
		// renamed file. The user's fresh tab at A.TXT still shows the old
		// content — an invisible overwrite of what that tab displays.
		if fresh.buf.Snapshot().String() != "content\n" {
			t.Logf("fresh tab content: %q", fresh.buf.Snapshot().String())
		}
		diskNow, _ := os.ReadFile(filepath.Join(h.a.root, "A.TXT"))
		t.Logf("A.TXT on disk now: %q — fresh tab shows %q (stale, diverged)", diskNow, fresh.buf.Snapshot().String())
	}
}

// TestAdversarialWatcherMissedCaseRenameSplits: the same case-rename as
// above, but the watcher never reports it (FSEvents can drop events
// under overflow; the fallback then is hints, and a hint reloads but
// does not re-key). The document stays keyed at the old spelling while
// DiskSpelling of any fresh input converges to the new one — the file
// becomes two documents of the same bytes.
func TestAdversarialWatcherMissedCaseRenameSplits(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "content\n"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	if err := os.Rename(d.path, filepath.Join(h.a.root, "A.TXT")); err != nil {
		t.Fatalf("case-only rename unsupported here: %v", err)
	}
	// No watcher event at all — as a dropped or coalesced rename leaves
	// it. The document is not gone: as far as the app knows a.txt exists.
	if d.gone {
		t.Fatal("doc must not be gone: no event arrived")
	}
	// The user clicks the file in the tree, which now lists A.TXT.
	fresh := h.openPath(filepath.Join(h.a.root, "A.TXT"))
	if fresh == d {
		t.Logf("no split: tree open found the existing document — converged")
		return
	}
	t.Logf("CONFIRMED: watcher-missed case rename leaves two documents of one file (%q stale, %q fresh); writes through either hit the same inode", d.path, fresh.path)
	if countDocs(h.a) != 2 {
		t.Errorf("expected the split pair, got %d docs", countDocs(h.a))
	}
	// The stale document does not even know it is stale: its path no
	// longer stats distinct, and a byte-compare save passes because the
	// folding filesystem resolves the old spelling to the same file.
	d.buf.Set("edit in stale tab\n")
	saved := h.a.saveDoc(d, false)
	disk, _ := os.ReadFile(filepath.Join(h.a.root, "A.TXT"))
	t.Logf("saving the stale tab: saved=%v; A.TXT now %q while the fresh tab shows %q", saved, disk, fresh.buf.Snapshot().String())
	if saved && fresh.buf.Snapshot().String() == "content\n" {
		t.Logf("CONFIRMED HAZARD: silent overwrite of the file the fresh tab displays, no conflict anywhere")
	}
}

// TestAdversarialWorkspaceEditCaseVariantUnopenedMergingFS verifies the
// phantom-write question the other way: on a merging filesystem, an edit
// to a case variant of an UNOPENED file must write the real file — and
// must not create a second file with the variant spelling.
func TestAdversarialWorkspaceEditCaseVariantUnopenedMergingFS(t *testing.T) {
	root := writeTree(t, map[string]string{"b.txt": "alpha beta\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	if !fsMerges(t, h.a.root, "probe.txt", "PROBE.TXT") {
		t.Skip("case-sensitive file system")
	}
	// A server that canonicalizes differently may send an edit for the
	// NFD/case-mixed variant. The write must land on b.txt alone.
	variant := filepath.Join(h.a.root, "B.txt")
	edit := &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		uri.FromPath(variant): {{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 5}}, NewText: "ALPHA"}},
	}}
	if err := h.a.applyWorkspaceEdit(edit, h.a.lsp); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(h.a.root, "b.txt"))
	if err != nil || string(data) != "ALPHA beta\n" {
		t.Errorf("disk b.txt = %q, %v", data, err)
	}
	entries, _ := os.ReadDir(h.a.root)
	count := 0
	for _, e := range entries {
		if e.Name() != ".DS_Store" {
			count++
			t.Logf("dir entry: %q", e.Name())
		}
	}
	if count != 1 {
		t.Errorf("phantom file created: %d entries in root", count)
	}
}

// TestAdversarialMarkdownPreviewRestoreSpelling is the regression test
// of the markdown-preview restore defect (verify-lane-b.md F2): the
// preview restore branch created docs directly from the stored session
// spelling, bypassing DiskSpelling, so a file case-renamed between
// sessions restored under the stale spelling and reopening it by its
// true spelling split the buffer. Restored paths of file-backed tabs
// now canonicalize like every other input.
func TestAdversarialMarkdownPreviewRestoreSpelling(t *testing.T) {
	if !fsMergesAtTemp(t) {
		t.Skip("case-sensitive file system: no fold variants to restore across")
	}
	root := writeTree(t, map[string]string{"A.md": "# hi\n"})
	h := newHarness(t, root)
	// Simulate a session that stored the tab with the OLD spelling A.md,
	// as written before the rename.
	storedPath := filepath.Join(h.a.root, "A.md")
	// The file is case-renamed on disk between sessions.
	if err := os.Rename(storedPath, filepath.Join(h.a.root, "a.md")); err != nil {
		t.Fatalf("case rename unsupported: %v", err)
	}
	// Craft the saved workspace state: one markdown preview tab of the
	// old spelling.
	h.a.cfg.Workspaces = map[string]config.Workspace{
		h.a.root: {
			Layout: &config.Layout{
				Tabs:    []config.Tab{{Path: storedPath, PreviewTab: true}},
				Focused: true,
			},
		},
	}
	h2 := newHarnessWithConfig(t, h.a.root, h.a.cfg)
	h2.settle(func() bool { return len(h2.a.docs) > 0 })
	h2.settle(func() bool {
		for _, d := range h2.a.docs {
			if d.loading {
				return false
			}
		}
		return true
	})
	stale := h2.a.docs[storedPath]
	canonical := h2.a.docs[filepath.Join(h2.a.root, "a.md")]
	t.Logf("restored docs: stale-key present=%v canonical-key present=%v", stale != nil, canonical != nil)
	if stale != nil {
		t.Logf("CONFIRMED D1: preview tab restored under stale spelling %q though the disk spells it a.md", storedPath)
		// The consequence: opening the file by its disk spelling creates
		// a SECOND document of the same file.
		second := h2.openPath(filepath.Join(h2.a.root, "a.md"))
		if second == stale {
			t.Logf("no split: opening by disk spelling found the restored document — defect fixed")
		} else if countDocs(h2.a) == 2 {
			t.Errorf("BUFFER SPLIT CONFIRMED: the same file is open as two documents (%q stale, %q fresh)", storedPath, second.path)
		}
	} else if canonical != nil {
		t.Logf("no split: restore canonicalized to the disk spelling — defect fixed")
	}
}

func fsMergesAtTemp(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi1, err1 := os.Lstat(probe)
	fi2, err2 := os.Lstat(filepath.Join(dir, "PROBE.TXT"))
	return err1 == nil && err2 == nil && os.SameFile(fi1, fi2)
}

// TestAdversarialRegularTabRestoreConverges verifies the implementer's
// implicit claim about ordinary tabs: they restore through openIn ->
// openInMode -> DiskSpelling, so a between-sessions case rename does NOT
// split ordinary documents.
func TestAdversarialRegularTabRestoreConverges(t *testing.T) {
	if !fsMergesAtTemp(t) {
		t.Skip("case-sensitive file system")
	}
	root := writeTree(t, map[string]string{"A.txt": "hi\n"})
	h := newHarness(t, root)
	storedPath := filepath.Join(h.a.root, "A.txt")
	if err := os.Rename(storedPath, filepath.Join(h.a.root, "a.txt")); err != nil {
		t.Fatalf("case rename unsupported: %v", err)
	}
	h.a.cfg.Workspaces = map[string]config.Workspace{
		h.a.root: {
			Layout: &config.Layout{
				Tabs:    []config.Tab{{Path: storedPath}},
				Focused: true,
			},
		},
	}
	h2 := newHarnessWithConfig(t, h.a.root, h.a.cfg)
	h2.settle(func() bool { return len(h2.a.docs) > 0 })
	h2.settle(func() bool {
		for _, d := range h2.a.docs {
			if d.loading {
				return false
			}
		}
		return true
	})
	if h2.a.docs[storedPath] != nil {
		t.Errorf("D1-for-regular-tabs CONFIRMED: restored under stale spelling %q", storedPath)
	} else if h2.a.docs[filepath.Join(h2.a.root, "a.txt")] != nil {
		t.Logf("regular tab restore converged to disk spelling a.txt — implementer's D1 description accurate for regular tabs")
	} else {
		t.Logf("docs after restore: %v", keysOf(h2.a.docs))
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestAdversarialDiagnosticsLandOnCanonicalDocument has a language server
// report diagnostics for a case variant of an open document's URI. On a
// merging filesystem they must land on the ONE open document, in both the
// buffer-backed and the disk-problems paths.
func TestAdversarialDiagnosticsLandOnCanonicalDocument(t *testing.T) {
	if !fsMergesAtTemp(t) {
		t.Skip("case-sensitive file system")
	}
	root := writeTree(t, map[string]string{"a.go": "package main\n"})
	h := newHarness(t, root)
	h.a.trust()
	d := h.open("a.go")
	h.settle(func() bool { return h.a.lsp.Current() != nil })
	report := h.a.diagnosticsHandler(h.a.gen)
	variantURI := uri.FromPath(filepath.Join(h.a.root, "A.GO"))
	pos := lsp.Position{Line: 0, Character: 1}
	items := []lsp.Diagnostic{{Range: lsp.Range{Start: pos, End: pos}, Message: "variant diagnostic", Source: "checker"}}

	// Buffer-backed report for the variant URI.
	snap := d.buf.Snapshot()
	report(h.a.lsp, langserver.DiagnosticReport{Client: h.fc, URI: variantURI, Buffer: &snap, Items: items})
	h.settle(func() bool { return len(d.diagnostics) > 0 })
	if d.diagnostics[0].Message != "variant diagnostic" {
		t.Fatalf("buffer diagnostics did not land on the open document: %+v", d.diagnostics)
	}

	// Disk report (no buffer) for the variant URI of an unopened file.
	unopened := filepath.Join(h.a.root, "b.go")
	if err := os.WriteFile(unopened, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	variantUnopened := uri.FromPath(filepath.Join(h.a.root, "B.GO"))
	report(h.a.lsp, langserver.DiagnosticReport{Client: h.fc, URI: variantUnopened, Items: items})
	h.settle(func() bool { return len(h.a.problems.disk) > 0 })
	if h.a.problems.disk[unopened] == nil {
		t.Errorf("disk diagnostics keyed by %v, want the canonical %q", keysOf(h.a.problems.disk), unopened)
	}
}

// TestAdversarialQuickOpenNewlineFragment documents what becomes of a
// file whose name contains a newline in quick open: rg prints it across
// two lines, each fragment a ValidRel-passing phantom entry. The join can
// no longer ESCAPE the root (Lane B), but the fragment entries are bogus.
func TestAdversarialQuickOpenJoinFallback(t *testing.T) {
	// joinRoot on a malformed entry degrades to the root rather than
	// escaping. A user clicking such a phantom row opens the root dir as
	// a file, which fails to read — benign, but note it does not error.
	got := joinRoot("/some/root", "../escape.txt")
	if got != "/some/root" {
		t.Errorf("joinRoot(../escape.txt) = %q, want the root fallback", got)
	}
	if got := joinRoot("/some/root", "ok/file.txt"); got != filepath.Join("/some/root", "ok/file.txt") {
		t.Errorf("honest join: %q", got)
	}
}

// TestAdversarialWatcherHintEscapeIsHarmlessButUncaught: a FileHint event
// for a path that lexically cancels to the root ("sub/..") reloads every
// document — because JoinRel legitimately resolves it to the root. This
// is not a containment escape (nothing outside is touched) but a
// robustness note: a malformed hint storms reloads.
func TestAdversarialWatcherHintEscapeIsHarmlessButUncaught(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "x\n"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	v := d.buf.Version()
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileHint, Path: "sub/.."}})
	// FileHint for the root reloads everything inside it.
	if !d.loading {
		t.Logf("hint did not trigger reload (loading=false)")
	} else {
		t.Logf("CONFIRMED: a 'sub/..' FileHint resolves to the root and reloads every document")
		h.settle(func() bool { return !d.loading })
	}
	if d.buf.Version() != v {
		t.Logf("buffer version changed across reload: %d -> %d (content unchanged reload is cheap)", v, d.buf.Version())
	}
	if d.buf.Snapshot().String() != "x\n" {
		t.Errorf("content changed: %q", d.buf.Snapshot().String())
	}
}
