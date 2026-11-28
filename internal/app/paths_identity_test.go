package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"see/internal/lsp"
	"see/internal/uri"
)

// fsMerges probes what the file system of dir does with a file reached
// by a variant spelling of its name: true when the variant stats to the
// file, as a case-insensitive or normalization-insensitive file system
// answers. Tests assert merging where the file system merges and
// distinctness where it does not, rather than assuming a platform.
func fsMerges(t *testing.T, dir, name, variant string) bool {
	t.Helper()
	probe := filepath.Join(dir, name)
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi1, err1 := os.Lstat(probe)
	fi2, err2 := os.Lstat(filepath.Join(dir, variant))
	// SameFile must be answered while the probe still exists: on
	// Windows it opens the file by its remembered path to compare
	// volume and file index, so removing the probe first answers a
	// merging file system as sensitive — the false negative the
	// variant branches of these tests then misread.
	same := err1 == nil && err2 == nil && os.SameFile(fi1, fi2)
	_ = os.Remove(probe)
	return same
}

// openPath opens p as the user would, from the tree or a drop, and
// answers the document it became once loaded — including one that
// failed to read, so a test can see what a spelling turned into.
func (h *harness) openPath(p string) *doc {
	h.t.Helper()
	h.a.userOpen(p)
	d := h.a.docs[h.a.activePath()]
	if d == nil {
		h.t.Fatalf("nothing opened for %s", p)
	}
	h.settle(func() bool { return !d.loading })
	return d
}

// countDocs is how many open documents the app holds.
func countDocs(a *App) int { return len(a.docs) }

// TestOpenCaseVariantSharesOneDocument opens a file and then a case
// variant of its path. Where the file system merges spellings, both
// opens are one document — never two buffers editing the same bytes;
// where it does not, the variant names another file, which fails to
// read rather than shadowing the first.
func TestOpenCaseVariantSharesOneDocument(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "hello\n"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	canonical := filepath.Join(h.a.root, "a.txt")
	if d.path != canonical {
		t.Fatalf("doc path %q, want %q", d.path, canonical)
	}
	variant := filepath.Join(h.a.root, "A.TXT")
	if !fsMerges(t, h.a.root, "probe.txt", "PROBE.TXT") {
		other := h.openPath(variant)
		if other == d {
			t.Fatal("case-sensitive file system merged two spellings")
		}
		if other.err == nil {
			t.Fatalf("the variant names no file here, yet read: %v", other.err)
		}
		return
	}
	again := h.openPath(variant)
	if again != d {
		t.Fatalf("case variant opened a second document: %q vs %q", again.path, d.path)
	}
	if countDocs(h.a) != 1 {
		t.Fatalf("%d documents of one file", countDocs(h.a))
	}
	if h.a.activePath() != canonical {
		t.Fatalf("active path %q, want the disk spelling %q", h.a.activePath(), canonical)
	}
}

// TestOpenUnicodeVariantSharesOneDocument is the case test for Unicode
// normalization: café spelled NFC and NFD is one file where the file
// system folds normalization, two names where it does not.
func TestOpenUnicodeVariantSharesOneDocument(t *testing.T) {
	nfc := "caf\u00e9.go"
	nfd := "cafe\u0301.go"
	root := writeTree(t, map[string]string{nfc: "package main\n"})
	h := newHarness(t, root)
	d := h.open(nfc)
	variant := filepath.Join(h.a.root, nfd)
	if !fsMerges(t, h.a.root, "p\u00e9.txt", "pe\u0301.txt") {
		other := h.openPath(variant)
		if other == d {
			t.Fatal("normalization-sensitive file system merged two spellings")
		}
		if other.err == nil {
			t.Fatalf("the variant names no file here, yet read: %v", other.err)
		}
		return
	}
	again := h.openPath(variant)
	if again != d {
		t.Fatalf("Unicode variant opened a second document: %q vs %q", again.path, d.path)
	}
	if countDocs(h.a) != 1 {
		t.Fatalf("%d documents of one file", countDocs(h.a))
	}
}

// TestRealPathOfSymlinkedRootIsOneDocument opens a workspace through a
// symlink, then a file in it by the physical path, as a language server
// reports one: both spellings are the one identity of the document,
// the workspace's own.
func TestRealPathOfSymlinkedRootIsOneDocument(t *testing.T) {
	real := writeTree(t, map[string]string{"a.txt": "hello\n"})
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	h := newHarness(t, link)
	if h.a.realRoot == h.a.root {
		t.Fatalf("the symlinked root did not resolve: %q", h.a.realRoot)
	}
	d := h.open("a.txt")
	if d.path != filepath.Join(h.a.root, "a.txt") {
		t.Fatalf("doc opened by %q, want the workspace's spelling", d.path)
	}
	physical, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	again := h.openPath(filepath.Join(physical, "a.txt"))
	if again != d {
		t.Fatalf("the physical path opened a second document: %q vs %q", again.path, d.path)
	}
	if countDocs(h.a) != 1 {
		t.Fatalf("%d documents of one file", countDocs(h.a))
	}
}

// TestInteriorSymlinkIsOwnLogicalIdentity pins the documented contract
// for a symlink inside the workspace: the path through it is a second
// logical identity of the same bytes, spelled as the user opened it,
// never silently merged — and each identity stays internally
// consistent, the one file's bytes in both.
func TestInteriorSymlinkIsOwnLogicalIdentity(t *testing.T) {
	root := writeTree(t, map[string]string{"real.txt": "same bytes\n"})
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	h := newHarness(t, root)
	d := h.open("real.txt")
	viaLink := h.openPath(filepath.Join(h.a.root, "link.txt"))
	if viaLink == d {
		t.Fatal("an interior symlink silently merged into its target's identity")
	}
	if countDocs(h.a) != 2 {
		t.Fatalf("%d documents, want the two documented identities", countDocs(h.a))
	}
	if viaLink.buf == nil || viaLink.buf.Snapshot().String() != "same bytes\n" {
		t.Fatalf("the link identity read %q", viaLink.buf.Snapshot().String())
	}
	if d.buf.Snapshot().String() != viaLink.buf.Snapshot().String() {
		t.Fatal("the two identities of one file disagree")
	}
}

// TestEntryPromptConfinedToItsFolder types paths into the new-file
// prompt: anything reaching out of the folder is rejected with the file
// uncreated, while an ordinary nested path is made where it says.
func TestEntryPromptConfinedToItsFolder(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "x\n"})
	h := newHarness(t, root)
	outside := filepath.Dir(root)
	marker := filepath.Join(outside, "evil-entry.txt")
	_ = os.Remove(marker)
	for _, name := range []string{
		"../evil-entry.txt", "sub/../../evil-entry.txt", "..", ".", "/etc/passwd",
		// Windows-only in effect: Win32 strips the trailing space of `.. `
		// at open time, which turns the component into a parent traversal.
		// Refused on every platform, as the strip is the volume namespace's,
		// not the client OS's (an SMB or exFAT volume shares it everywhere).
		".. /evil-entry.txt",
		"notes.",
	} {
		q := &entryPrompt{dir: h.a.root, name: name}
		h.a.createEntry = q
		h.a.submitEntry(q)
		if q.err == "" {
			t.Errorf("%q was accepted", name)
		}
		if _, err := os.Lstat(marker); err == nil {
			t.Fatalf("%q created %s", name, marker)
		}
	}
	q := &entryPrompt{dir: h.a.root, name: "sub/ok.txt"}
	h.a.createEntry = q
	h.a.submitEntry(q)
	if q.err != "" {
		t.Fatalf("a plain nested path rejected: %q", q.err)
	}
	h.settle(func() bool {
		_, err := os.Lstat(filepath.Join(h.a.root, "sub", "ok.txt"))
		return err == nil
	})
}

// TestWatcherEventsAreConfinedToTheRoot feeds the watch a removal whose
// path climbs out of the root. A document open on the outside file must
// survive it: the join is confined, so a malformed report can neither
// read nor mark anything outside the workspace — while a honest removal
// inside it still lands.
func TestWatcherEventsAreConfinedToTheRoot(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "inside\n"})
	h := newHarness(t, root)
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "out.txt")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inner := h.open("a.txt")
	outer := h.openPath(outside)
	if outer.buf == nil {
		t.Fatal("the outside file did not open")
	}
	rel, err := filepath.Rel(h.a.root, outside)
	if err != nil || !strings.HasPrefix(rel, "..") {
		t.Fatalf("the outside file is not outside: %q", rel)
	}
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileRemove, Path: filepath.ToSlash(rel)}})
	if outer.gone {
		t.Error("a watch report escaping the root marked the outside document gone")
	}
	if inner.gone {
		t.Error("the escaping report touched the inside document")
	}
	// Control: an honest removal inside the root still lands.
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileRemove, Path: "a.txt"}})
	if !inner.gone {
		t.Error("an honest removal did not mark its document gone")
	}
}

// TestWatcherRenameClosesOneIdentityAndOpensNone renames a file on disk
// and feeds the watch's rename report: the document of the old path is
// gone where it stands, nothing opens itself at the new path — one
// file, never two buffers — and opening the new path is a fresh
// document, as the contract documents of watcher renames.
func TestWatcherRenameClosesOneIdentityAndOpensNone(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "watched\n"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	oldPath := d.path
	if err := os.Rename(oldPath, filepath.Join(h.a.root, "b.txt")); err != nil {
		t.Fatal(err)
	}
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileRename, OldPath: "a.txt", Path: "b.txt"}})
	if !d.gone {
		t.Error("the renamed-away document is not gone")
	}
	if d.path != oldPath {
		t.Errorf("the document moved to %q: a watcher rename must not continue an identity", d.path)
	}
	if next := h.a.docs[filepath.Join(h.a.root, "b.txt")]; next != nil {
		t.Error("the rename opened the new path by itself, duplicating the file")
	}
	if countDocs(h.a) != 1 {
		t.Errorf("%d documents, want the one the user opened", countDocs(h.a))
	}
	fresh := h.open("b.txt")
	if fresh == d {
		t.Error("opening the new path resurrected the closed identity")
	}
}

// TestInAppRenameContinuesTheDocument pins the other half of the rename
// contract: a rename performed in the app keeps the document, its
// buffer and its tab, at the new path.
func TestInAppRenameContinuesTheDocument(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "continuity\n"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	buf := d.buf
	next := filepath.Join(h.a.root, "renamed.txt")
	h.a.renamed(d.path, next)
	if h.a.docs[next] != d {
		t.Fatal("the document did not follow its rename")
	}
	if d.path != next || d.buf != buf {
		t.Errorf("identity broken: path %q, buffer same %v", d.path, d.buf == buf)
	}
	if h.a.docs[filepath.Join(h.a.root, "a.txt")] != nil {
		t.Error("the old path kept a document")
	}
	if countDocs(h.a) != 1 {
		t.Errorf("%d documents of one file", countDocs(h.a))
	}
	if !h.a.focus.has(next) {
		t.Error("the tab did not follow the rename")
	}
}

// TestWorkspaceEditLandsOnTheOpenDocument has a language server edit a
// file by a case variant of its URI. Where the file system merges
// spellings the edit lands in the open buffer of the file, not in a
// second document or a phantom write; where it does not, nothing is
// written anywhere.
func TestWorkspaceEditLandsOnTheOpenDocument(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "hello world\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	d := h.open("a.txt")
	variant := filepath.Join(h.a.root, "A.TXT")
	edit := &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		uri.FromPath(variant): {{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 6}, End: lsp.Position{Line: 0, Character: 11}}, NewText: "there"}},
	}}
	err := h.a.applyWorkspaceEdit(edit, h.a.lsp)
	if !fsMerges(t, h.a.root, "probe.txt", "PROBE.TXT") {
		if err == nil {
			t.Error("an edit of a nonexistent variant applied")
		}
		if d.buf.Snapshot().String() != "hello world\n" {
			t.Error("the buffer changed through a variant that names nothing here")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := d.buf.Snapshot().String(); got != "hello there\n" {
		t.Errorf("buffer %q, want the edit in the open document", got)
	}
	if countDocs(h.a) != 1 {
		t.Errorf("%d documents of one file", countDocs(h.a))
	}
	// The disk keeps its bytes until the user saves: the edit went to
	// the buffer, not around it.
	if data, _ := os.ReadFile(d.path); string(data) != "hello world\n" {
		t.Errorf("disk %q changed under an unsaved buffer", data)
	}
}

// TestWorkspaceEditOfUnopenedCanonicalFile writes through a server edit
// of a file no tab holds, spelled as a variant: where spellings merge
// the one file on disk changes, and no second file appears.
func TestWorkspaceEditOfUnopenedCanonicalFile(t *testing.T) {
	root := writeTree(t, map[string]string{"b.txt": "alpha beta\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	if !fsMerges(t, h.a.root, "probe.txt", "PROBE.TXT") {
		t.Skip("case-sensitive file system")
	}
	variant := filepath.Join(h.a.root, "B.TXT")
	edit := &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		uri.FromPath(variant): {{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 6}, End: lsp.Position{Line: 0, Character: 10}}, NewText: "gamma"}},
	}}
	if err := h.a.applyWorkspaceEdit(edit, h.a.lsp); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(h.a.root, "b.txt"))
	if err != nil || string(data) != "alpha gamma\n" {
		t.Errorf("disk %q, %v", data, err)
	}
	if countDocs(h.a) != 0 {
		t.Errorf("a document opened for an edit of disk: %d", countDocs(h.a))
	}
}
