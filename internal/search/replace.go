package search

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var (
	ErrStaleMatches = errors.New("file changed since it was searched")
	ErrNotText      = errors.New("not a supported UTF-8 text file")
)

// ReplaceFile applies the displayed matches to one file, or leaves it intact.
// Replacement is literal even when the query was a regular expression.
func ReplaceFile(ctx context.Context, root *os.Root, rel string, matches []Match, replacement string) (int, error) {
	if err := ctx.Err(); err != nil || len(matches) == 0 {
		return 0, err
	}
	if !filepath.IsLocal(rel) || !utf8.ValidString(replacement) || strings.ContainsRune(replacement, 0) {
		return 0, ErrNotText
	}
	dir, name := filepath.Split(filepath.Clean(rel))
	parent, err := root.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return 0, err
	}
	defer parent.Close()
	before, info, err := readReplaceTarget(parent, name)
	if err != nil {
		return 0, err
	}
	after, count, err := replaceMatches(before, matches, replacement)
	if err != nil || count == 0 {
		return 0, err
	}

	tmpName := ".see-replace-" + rand.Text()
	tmp, err := parent.OpenFile(tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	defer parent.Remove(tmpName)
	if _, err = tmp.WriteString(after); err == nil {
		err = tmp.Chmod(info.Mode().Perm())
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}
	current, currentInfo, err := readReplaceTarget(parent, name)
	if err != nil {
		return 0, err
	}
	if current != before || !os.SameFile(info, currentInfo) || info.Mode() != currentInfo.Mode() {
		return 0, ErrStaleMatches
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := parent.Rename(tmpName, name); err != nil {
		return 0, err
	}
	return count, nil
}

func readReplaceTarget(parent *os.Root, name string) (string, os.FileInfo, error) {
	info, err := parent.Lstat(name)
	if err != nil {
		return "", nil, err
	}
	const maxBytes = 4 << 20 // same limit as ripgrep
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return "", nil, ErrNotText
	}
	if info.Mode().Perm()&0o222 == 0 {
		return "", nil, os.ErrPermission
	}
	f, err := parent.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	if !os.SameFile(info, opened) {
		return "", nil, ErrStaleMatches
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(b) > maxBytes || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return "", nil, ErrNotText
	}
	return string(b), info, nil
}

func replaceMatches(text string, matches []Match, replacement string) (string, int, error) {
	var out strings.Builder
	off := 0
	// Ripgrep's byte coordinates exclude the UTF-8 BOM.
	if strings.HasPrefix(text, "\ufeff") {
		off = len("\ufeff")
	}
	line, start, copied, count := 0, off, 0, 0
	var end, checkedLine int
	checkedLine = -1
	for _, m := range matches {
		if m.Line < line {
			return "", 0, ErrStaleMatches
		}
		for line < m.Line {
			n := strings.IndexByte(text[start:], '\n')
			if n < 0 {
				return "", 0, ErrStaleMatches
			}
			start += n + 1
			line++
		}
		if checkedLine != line {
			end = len(text)
			if n := strings.IndexByte(text[start:], '\n'); n >= 0 {
				end = start + n + 1
			}
			source := trimLineEnding(text[start:end])
			if sha256.Sum256([]byte(source)) != m.lineHash {
				return "", 0, ErrStaleMatches
			}
			end = start + len(source)
			checkedLine = line
		}
		from, to := start+m.Col, start+m.End
		if m.Col < 0 || from < copied || from >= to || to > end || !utf8.RuneStart(text[from]) || (to < end && !utf8.RuneStart(text[to])) {
			return "", 0, ErrStaleMatches
		}
		if text[from:to] == replacement {
			continue
		}
		out.WriteString(text[copied:from])
		out.WriteString(replacement)
		copied = to
		count++
	}
	if count == 0 {
		return text, 0, nil
	}
	out.WriteString(text[copied:])
	return out.String(), count, nil
}
