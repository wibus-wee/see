package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Paths of a workspace: the identity of its folder and of its documents.
//
// The identity policy of the app, which every boundary normalizes into
// and every cache keys by:
//
//   - Identity is logical: the absolute, clean path the user reaches a
//     file by, spelled as the disk spells it. A document opened through
//     the symlinked root is mapped back under the root the user opened
//     (DocPath), so both spellings of the root are one identity. A file
//     reached through a symlink inside the workspace keeps the spelling
//     it was opened by: that is a second logical identity of the same
//     physical file, as in other editors, never a silent merge.
//   - On case-insensitive or normalization-insensitive file systems the
//     disk spelling is the canonical one, so that a path spelled any
//     other way (typed, dragged, reported by a language server) still
//     finds the open document of the file it names. DiskSpelling and
//     FoldEqual probe the file system instead of assuming a platform's
//     semantics, as the spec's case policy demands.
//   - What tools write (git status, a file watch, search results) is a
//     slash-separated path relative to a root, joined with JoinRel,
//     which rejects anything that would land outside the root.
//
// Renaming a file inside the app continues the identity of its open
// documents; a rename the watcher reports is the old identity closing
// and a new one opening, because a replaced path is how builds and
// atomic saves of other tools publish, and the replacement is not the
// document the user edited.

// CanonicalRoot makes a workspace's path absolute and clean, without
// resolving symlinks, so the tree shows the path the user chose. Its
// spelling is the disk's, so that a folder reached by a case or Unicode
// variant of its name is the same root — the same session, the same
// recents — as the folder itself.
func CanonicalRoot(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", &os.PathError{Op: "open", Path: abs, Err: ErrNotDir}
	}
	return filepath.Clean(DiskSpelling(abs)), nil
}

// ErrNotDir is the error CanonicalRoot gives for a path that is not a
// directory.
var ErrNotDir = errors.New("not a directory")

// Within reports whether p is root or inside it.
func Within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// DocPath is the identity of the document of a path: the path clean and
// absolute, and, for a path a language server gives through the real
// location of a symlinked workspace (as macOS's /tmp is /private/tmp),
// the same path under the workspace as the user opened it.
func DocPath(root, realRoot, p string) string {
	p = filepath.Clean(p)
	if root != "" && realRoot != "" && realRoot != root && Within(realRoot, p) && !Within(root, p) {
		if rel, err := filepath.Rel(realRoot, p); err == nil {
			return filepath.Join(root, rel)
		}
	}
	return p
}

// RelPath is p relative to root for showing, or p itself outside it.
func RelPath(root, p string) string {
	if root != "" && Within(root, p) {
		if rel, err := filepath.Rel(root, p); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return p
}

// ErrOutsideRoot is the error a confined join gives for input that
// would land outside its root: a parent traversal out of it, or an
// absolute or volume-anchored path.
var ErrOutsideRoot = errors.New("path escapes the root")

// cleanRel normalizes rel, a slash-separated relative path as tools
// write one: repeated separators collapse, "." components drop and
// ".." cancels what precedes it, like path.Clean but through the OS's
// separators. The result is "" for the root itself, or a path
// without a leading separator.
//
// Components ending in a dot or a space are refused outright, on every
// platform: Win32 normalization strips them at open time, so a lexical
// join cannot confine them — ".. " reads as ".." — and while POSIX
// file systems carry such names literally, a volume shared with Windows
// (SMB, exFAT) does not, which leaves no platform the app runs on where
// joining them is safe. Almost nothing real is lost: trailing-dot and
// trailing-space names are absent from ordinary projects and impossible
// to create on Windows.
func cleanRel(rel string) (string, bool) {
	if strings.ContainsRune(rel, '\x00') {
		return "", false
	}
	rel = filepath.FromSlash(rel)
	if filepath.IsAbs(rel) {
		return "", false
	}
	if filepath.VolumeName(rel) != "" {
		// A drive letter makes the input anchored to a volume: C:\x is
		// absolute, C:x is relative to the drive's own working
		// directory, and joining either under the root rewrites what it
		// names. The colon is no component of a portable relative path
		// anyway.
		return "", false
	}
	if runtime.GOOS == "windows" && strings.HasPrefix(rel, "\\") {
		// A leading separator is volume-anchored on Windows (\x is
		// the current drive's root) even where filepath.IsAbs cannot
		// call it absolute, since the drive is missing. UNC roots
		// (\\server\share) share the leading separator.
		return "", false
	}
	var parts []string
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		switch part {
		case "", ".":
			// Repeated separators and dot components vanish.
		case "..":
			if len(parts) == 0 {
				return "", false
			}
			parts = parts[:len(parts)-1]
		default:
			if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
				return "", false
			}
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, string(filepath.Separator)), true
}

// JoinRel joins rel, a slash-separated path relative to root as tools
// write one, to root, confined to it: input that is absolute,
// volume-anchored, NUL-bearing, escapes through ".." or carries a
// component the Win32 namespace would rewrite (trailing dot or space)
// is refused with ErrOutsideRoot, never silently re-rooted. Input of "", "." or
// separators alone is the root itself, as a file watch reports the
// watched directory. The check is lexical, as OpenDAL documents of its
// own confined join: a symlink inside the root can point outside it,
// which joining cannot prevent and opening must not treat as an
// escape, since following the user's links is the point.
func JoinRel(root, rel string) (string, error) {
	clean, ok := cleanRel(rel)
	if !ok {
		return "", ErrOutsideRoot
	}
	if clean == "" {
		return root, nil
	}
	return filepath.Join(root, clean), nil
}

// ValidRel reports whether rel is a well-formed root-relative path that
// names something inside its root, as git writes one: slash-separated,
// relative, and confined. Unlike JoinRel it requires rel to name
// something below the root, not the root itself.
func ValidRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return false
	}
	clean, ok := cleanRel(rel)
	return ok && clean != "" && clean != "."
}

// foldKey is the key two spellings of one file share on a
// case-insensitive, normalization-insensitive file system: Unicode NFC
// and full case folding — not simple case mapping, as file systems
// full-fold too, and spellings simple mapping keeps distinct (Turkish
// İ, uppercase ẞ, the Greek prosgegrammeni blocks) are one file there,
// which a narrow key would split into two documents. It is a candidate
// relation only — whether two spellings really are one file is the file
// system's to say, which is why lookups probe with os.SameFile or the
// directory's own listing rather than trusting this key.
func foldKey(name string) string {
	return folder.String(string(norm.NFC.Bytes([]byte(name))))
}

var folder = cases.Fold()

// FoldEqual reports whether a and b could spell one file on a
// case-insensitive, normalization-insensitive file system. It answers
// identity questions of candidates only; two paths may be distinct on
// a case-sensitive file system even when FoldEqual holds.
func FoldEqual(a, b string) bool {
	return foldKey(a) == foldKey(b)
}

// DiskSpelling is p with the case and Unicode normalization the disk
// actually spells each existing component by, so that paths reached by
// any spelling of a case-insensitive or normalization-insensitive file
// system converge on one identity. Components that do not exist keep
// their spelling, and once one does, nothing deeper can be probed. The
// answer is p itself when it is already the disk's spelling. Every
// component is probed, exact spelling or not — there is no fast path —
// which on a network file system costs a directory listing per ancestor.
func DiskSpelling(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	vol := filepath.VolumeName(abs)
	rest := abs[len(vol):]
	rooted := strings.HasPrefix(rest, string(filepath.Separator))
	cur := vol + string(filepath.Separator)
	var comps []string
	if rooted {
		comps = strings.Split(strings.TrimPrefix(rest, string(filepath.Separator)), string(filepath.Separator))
	} else {
		// A drive-relative Windows path: its meaning depends on the
		// drive's working directory, which probing cannot honor.
		return p
	}
	// On Windows the root of the volume needs no probing; UNC roots
	// cannot be listed with their own spelling either way.
	for i, comp := range comps {
		if comp == "" {
			continue
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			break
		}
		exact := false
		candidate := ""
		for _, e := range entries {
			if e.Name() == comp {
				exact = true
				break
			}
			if candidate == "" && FoldEqual(e.Name(), comp) {
				candidate = e.Name()
			}
		}
		spelling := comp
		if !exact && candidate != "" {
			// A fold-equal entry is the same file only when the file
			// system itself resolves the input spelling to it: on a
			// case-insensitive or normalization-insensitive file system
			// the input stats to the same inode, while on a sensitive one
			// it stats nowhere or somewhere else, and the input spelling
			// stands, as two spellings may be two files there.
			inputStat, err1 := os.Lstat(filepath.Join(cur, comp))
			candStat, err2 := os.Lstat(filepath.Join(cur, candidate))
			if err1 == nil && err2 == nil && os.SameFile(inputStat, candStat) {
				spelling = candidate
			}
		}
		comps[i] = spelling
		cur = filepath.Join(cur, spelling)
	}
	if len(comps) == 0 || len(comps) == 1 && comps[0] == "" {
		return filepath.Clean(vol + string(filepath.Separator))
	}
	return filepath.Join(vol+string(filepath.Separator), filepath.Join(comps...))
}
