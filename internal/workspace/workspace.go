// Package workspace lists a workspace's directories lazily, one directory
// at a time, sorts their entries the same way on every platform, and
// classifies files before they are shown as source.
package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Entry is a file or directory in a directory.
type Entry struct {
	Name string
	Path string
	Dir  bool
	// Link reports a symbolic link; Dir is then whether it points to a
	// directory. Links are never followed by scans, only when the user
	// opens them.
	Link bool
}

// Ignored reports whether a directory is left out of watching and any
// scan by default, and shown dimmed: version control data, dependencies
// and build output.
func Ignored(name string) bool {
	return slices.Contains(IgnoredNames(), name)
}

// IgnoredNames lists the folders Ignored names.
func IgnoredNames() []string {
	return []string{".git", ".hg", ".svn", "node_modules", "dist", "build", ".idea", ".vscode-test", "__pycache__", ".cache"}
}

// IgnoredRel reports whether a slash-separated path relative to the
// workspace is in an ignored directory, or is one.
func IgnoredRel(rel string) bool {
	for part := range strings.SplitSeq(rel, "/") {
		if Ignored(part) {
			return true
		}
	}
	return false
}

// batch is how many entries ReadDir reads between checks of its context.
const batch = 512

// ReadDir lists the direct entries of dir, sorted with Less. It stops
// with ctx's error when ctx is canceled, between batches of entries, so a
// huge directory can be abandoned.
func ReadDir(ctx context.Context, dir string) ([]Entry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		des, err := f.ReadDir(batch)
		for _, de := range des {
			e := Entry{Name: de.Name(), Path: filepath.Join(dir, de.Name()), Dir: de.IsDir()}
			if de.Type()&fs.ModeSymlink != 0 {
				e.Link = true
				if fi, err := os.Stat(e.Path); err == nil {
					e.Dir = fi.IsDir()
				}
			}
			out = append(out, e)
		}
		if errors.Is(err, io.EOF) || (err == nil && len(des) == 0) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	Sort(out)
	return out, nil
}

// Sort orders entries with Less.
func Sort(es []Entry) {
	slices.SortStableFunc(es, func(a, b Entry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return Compare(a.Name, b.Name)
	})
}

// Compare orders names naturally and the same on every platform: case
// is ignored first (file2 < File10 < file10b), runs of digits compare by
// value, and names equal but for case or leading zeros fall back to
// their bytes, so the order is total.
func Compare(a, b string) int {
	if c := natural(a, b); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func natural(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return cmpInt(len(na), len(nb))
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		ra, wa := utf8.DecodeRuneInString(a[i:])
		rb, wb := utf8.DecodeRuneInString(b[j:])
		la, lb := unicode.ToLower(ra), unicode.ToLower(rb)
		if la != lb {
			return cmpInt(int(la), int(lb))
		}
		i += wa
		j += wb
	}
	return cmpInt(len(a)-i, len(b)-j)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Kind is what a file is, as far as showing it as source goes.
type Kind int

const (
	Text Kind = iota
	Binary
	BadEncoding
	TooLarge
)

// Size limits for showing files.
const (
	// PlainLimit is the size above which a file shows without syntax
	// highlighting.
	PlainLimit = 4 << 20
	// MaxSize is the largest file shown at all.
	MaxSize = 48 << 20
)

// File is a file read for showing.
type File struct {
	Text string
	Kind Kind
	Size int64
	// ModTime is the file's modification time as read.
	ModTime time.Time
	// Plain reports a file large enough to show without highlighting.
	Plain bool
}

// ErrNotRegular is returned for paths that are not regular files.
var ErrNotRegular = errors.New("not a regular file")

// ReadFile reads a file to show it as source. Files that are not text
// come back with their Kind and no Text; a UTF-8 byte order mark is
// dropped. Nothing is ever cut short.
func ReadFile(p string) (File, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return File{}, err
	}
	if !fi.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s: %w", path.Base(filepath.ToSlash(p)), ErrNotRegular)
	}
	f := File{Size: fi.Size(), ModTime: fi.ModTime()}
	if f.Size > MaxSize {
		f.Kind = TooLarge
		return f, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return File{}, err
	}
	f.Size = int64(len(data))
	if f.Size > MaxSize {
		f.Kind = TooLarge
		return f, nil
	}
	f.Kind = Classify(data)
	if f.Kind != Text {
		return f, nil
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	f.Text = string(data)
	f.Plain = f.Size > PlainLimit
	return f, nil
}

// Classify tells text from binary data and text in encodings other than
// UTF-8: a NUL byte in the first 8 KiB, or UTF-16's byte order marks,
// make it binary; invalid UTF-8 a bad encoding.
func Classify(data []byte) Kind {
	head := data[:min(len(data), 8<<10)]
	if bytes.HasPrefix(head, []byte{0xff, 0xfe}) || bytes.HasPrefix(head, []byte{0xfe, 0xff}) {
		return BadEncoding
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return Binary
	}
	if !utf8.Valid(data) {
		return BadEncoding
	}
	return Text
}
