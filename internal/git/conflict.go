package git

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrConflictUnsupported = errors.New("not a supported text conflict")
	ErrConflictMarkers     = errors.New("incomplete or nested conflict markers")
	ErrConflictStale       = errors.New("the working file or Git index changed")
	ErrConflictResolved    = errors.New("the file is no longer conflicted")
)

type ConflictChoice uint8

const utf8BOM = "\xef\xbb\xbf"

const (
	KeepCurrent ConflictChoice = iota + 1
	KeepIncoming
	KeepBoth
)

type ConflictHunk struct {
	Line                        int
	Current, Incoming           string
	CurrentLabel, IncomingLabel string
	start, end                  int
}

func (h ConflictHunk) Result(choice ConflictChoice) string {
	switch choice {
	case KeepCurrent:
		return h.Current
	case KeepIncoming:
		return h.Incoming
	case KeepBoth:
		if h.start == 0 && strings.HasPrefix(h.Current, utf8BOM) {
			return h.Current + strings.TrimPrefix(h.Incoming, utf8BOM)
		}
		return h.Current + h.Incoming
	default:
		return ""
	}
}

type ConflictFile struct {
	Hunks                   []ConflictHunk
	root, path, text, index string
	mode                    os.FileMode
}

// Symlinks and submodules need a path-level resolution rather than text choices.
func conflictIndex(ctx context.Context, root, path string) (string, error) {
	out, err := Command(ctx, root, "ls-files", "-u", "-z", "--", path)
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", ErrConflictResolved
	}
	for rec := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		meta, name, ok := strings.Cut(rec, "\t")
		mode, _, _ := strings.Cut(meta, " ")
		if !ok || name != path || mode != "100644" && mode != "100755" {
			return "", ErrConflictUnsupported
		}
	}
	return string(out), nil
}

const conflictFileLimit = 4 << 20

func readConflictText(root *os.Root, path string) (string, os.FileMode, error) {
	info, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, ErrConflictUnsupported
	}
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > conflictFileLimit {
		return "", 0, ErrConflictUnsupported
	}
	f, err := root.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	if opened, err := f.Stat(); err != nil || !os.SameFile(info, opened) {
		return "", 0, ErrConflictStale
	}
	b, err := io.ReadAll(io.LimitReader(f, conflictFileLimit+1))
	if err != nil {
		return "", 0, err
	}
	if len(b) > conflictFileLimit || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return "", 0, ErrConflictUnsupported
	}
	return string(b), info.Mode(), nil
}

func LoadConflict(ctx context.Context, root, path string) (*ConflictFile, error) {
	if !filepath.IsLocal(filepath.FromSlash(path)) {
		return nil, ErrConflictUnsupported
	}
	index, err := conflictIndex(ctx, root, path)
	if err != nil {
		return nil, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	text, mode, err := readConflictText(dir, path)
	if err != nil {
		return nil, err
	}

	width := 7
	out, err := Command(ctx, root, "check-attr", "-z", "merge", "conflict-marker-size", "--", path)
	if err != nil {
		return nil, err
	}
	if fields := strings.Split(string(out), "\x00"); len(fields) == 7 {
		if fields[2] == "unset" || fields[2] == "binary" {
			return nil, ErrConflictUnsupported
		}
		if n, err := strconv.Atoi(fields[5]); err == nil && n > 0 {
			width = n
		}
	}
	hunks, err := parseConflictHunks(text, width)
	if err != nil {
		return nil, err
	}
	if len(hunks) > 0 {
		hunks, err = excludeConflictExamples(ctx, root, index, text, width, hunks)
		if err != nil {
			return nil, err
		}
	}
	return &ConflictFile{Hunks: hunks, root: root, path: path, text: text, index: index, mode: mode}, nil
}

// A complete marker block already in both sides may be literal file content.
func excludeConflictExamples(ctx context.Context, root, index, text string, width int, hunks []ConflictHunk) ([]ConflictHunk, error) {
	var sides [2]map[string]int
	for rec := range strings.SplitSeq(index, "\x00") {
		meta, _, _ := strings.Cut(rec, "\t")
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[2] != "2" && fields[2] != "3" {
			continue
		}
		oid := fields[1]
		size, err := Command(ctx, root, "cat-file", "-s", oid)
		if err != nil {
			return nil, err
		}
		if n, err := strconv.Atoi(strings.TrimSpace(string(size))); err != nil || n > conflictFileLimit {
			return nil, ErrConflictUnsupported
		}
		blob, err := Command(ctx, root, "cat-file", "blob", oid)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(blob) || bytes.IndexByte(blob, 0) >= 0 {
			return nil, ErrConflictUnsupported
		}
		content := string(blob)
		blocks, err := parseConflictHunks(content, width)
		if err != nil {
			return nil, err
		}
		sides[fields[2][0]-'2'] = conflictBlockCounts(content, blocks)
	}
	if sides[0] == nil || sides[1] == nil {
		return nil, ErrConflictUnsupported
	}
	work := conflictBlockCounts(text, hunks)
	kept := hunks[:0]
	for _, h := range hunks {
		block := conflictBlock(text, h)
		if common := min(sides[0][block], sides[1][block]); common > 0 {
			if work[block] > common {
				return nil, ErrConflictMarkers
			}
			continue
		}
		kept = append(kept, h)
	}
	return kept, nil
}

func conflictBlockCounts(text string, hunks []ConflictHunk) map[string]int {
	counts := make(map[string]int, len(hunks))
	for _, h := range hunks {
		counts[conflictBlock(text, h)]++
	}
	return counts
}

func conflictBlock(text string, h ConflictHunk) string {
	return strings.ReplaceAll(text[h.start:h.end], "\r\n", "\n")
}

func parseConflictHunks(text string, width int) ([]ConflictHunk, error) {
	var hunks []ConflictHunk
	var h ConflictHunk
	pos, lineNo := 0, 0
	if strings.HasPrefix(text, utf8BOM) {
		pos = len(utf8BOM)
	}
	size, currentStart, incomingStart, section := 0, 0, 0, byte(0)
	for raw := range strings.SplitAfterSeq(text[pos:], "\n") {
		if raw == "" {
			break
		}
		lineNo++
		next := pos + len(raw)
		line := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		kind, count, label := conflictMarker(line)
		start := kind == '<' && (count == width || count >= 7)
		if section == 0 {
			if start {
				h = ConflictHunk{Line: lineNo, start: pos, CurrentLabel: label}
				size, currentStart, section = count, next, '<'
			} else if (kind == '>' || kind == '|') && (count == width || count >= 7) && label != "" {
				return nil, ErrConflictMarkers
			}
		} else if start {
			return nil, ErrConflictMarkers
		} else if count == size {
			switch {
			case kind == '|' && section == '<':
				h.Current = text[currentStart:pos]
				section = '|'
			case kind == '=' && (section == '<' || section == '|'):
				if section == '<' {
					h.Current = text[currentStart:pos]
				}
				incomingStart, section = next, '='
			case kind == '>' && section == '=':
				h.Incoming, h.IncomingLabel, h.end = text[incomingStart:pos], label, next
				hunks = append(hunks, h)
				section = 0
			case kind != 0:
				return nil, ErrConflictMarkers
			}
		}
		pos = next
	}
	if section != 0 {
		return nil, ErrConflictMarkers
	}
	return hunks, nil
}

func conflictMarker(line string) (kind byte, count int, label string) {
	if line == "" || !strings.ContainsRune("<|=>", rune(line[0])) {
		return 0, 0, ""
	}
	kind, count = line[0], 1
	for count < len(line) && line[count] == kind {
		count++
	}
	if count == len(line) {
		return kind, count, ""
	}
	if kind != '=' && line[count] == ' ' {
		return kind, count, strings.TrimSpace(line[count:])
	}
	return 0, 0, ""
}

func (f *ConflictFile) result(choices []ConflictChoice) (string, error) {
	if len(choices) == 0 || len(choices) != len(f.Hunks) {
		return "", ErrConflictMarkers
	}
	var out strings.Builder
	last := 0
	for i, h := range f.Hunks {
		if choices[i] < KeepCurrent || choices[i] > KeepBoth {
			return "", ErrConflictMarkers
		}
		out.WriteString(f.text[last:h.start])
		out.WriteString(h.Result(choices[i]))
		last = h.end
	}
	out.WriteString(f.text[last:])
	return out.String(), nil
}

// Resolve rechecks the snapshot before replacing the worktree file, then
// stages only this path. If staging fails, the resolved text stays on disk.
func (f *ConflictFile) Resolve(ctx context.Context, choices []ConflictChoice) error {
	text, err := f.result(choices)
	if err != nil {
		return err
	}
	dir, err := os.OpenRoot(f.root)
	if err != nil {
		return err
	}
	defer dir.Close()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		index, err := conflictIndex(ctx, f.root, f.path)
		if err != nil {
			return err
		}
		current, mode, err := readConflictText(dir, f.path)
		if errors.Is(err, ErrConflictUnsupported) {
			return ErrConflictStale
		}
		if err != nil {
			return err
		}
		if index != f.index || current != f.text || mode != f.mode {
			return ErrConflictStale
		}
		return nil
	}
	if text != f.text {
		name := filepath.Join(filepath.Dir(f.path), ".see-merge-"+rand.Text())
		tmp, err := dir.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, f.mode.Perm())
		if err != nil {
			return err
		}
		defer dir.Remove(name)
		_, err = io.WriteString(tmp, text)
		if err == nil {
			err = tmp.Chmod(f.mode.Perm())
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		if err := dir.Rename(name, f.path); err != nil {
			return err
		}
	} else if err := check(); err != nil {
		return err
	}
	if _, err := Command(ctx, f.root, "add", "--", f.path); err != nil {
		return fmt.Errorf("stage %q: %w", f.path, err)
	}
	return nil
}
