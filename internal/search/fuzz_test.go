package search

import (
	"crypto/sha256"
	"strings"
	"testing"
	"unicode/utf8"
)

// The fuzz targets of this package follow docs/specs/conformance/lane-e-fuzzing.md:
// inputs are structured toward valid or near-valid shapes, budgets cap what
// one input can make the code do, and assertions are about public behavior —
// no panic, bounded output, coordinates that stay inside the text they point
// into — never about mirroring internals. Inputs that crash land in
// testdata/fuzz/<target>/ as permanent regression tests.

// FuzzPreview exercises the byte slicing of long-line previews. start/end
// are kept inside the line, as ripgrep's submatches always are, with the
// harness occasionally nudging them out of contract: the function is not a
// boundary parser, but out-of-contract input must fail as a panic-free
// wrong answer at worst — never as memory corruption or a slice explosion.
func FuzzPreview(f *testing.F) {
	f.Add("hello world, hello again", 0, 5)
	f.Add(strings.Repeat("x", 300)+"hello"+strings.Repeat("y", 300), 300, 305)
	f.Add("你好 hello 你好", 7, 12)
	f.Add("", 0, 0)
	f.Add("abc", -5, 999)
	f.Fuzz(func(t *testing.T, text string, start, end int) {
		// Budget: the preview contract is about lines ripgrep matched,
		// which are at most its 4000-byte column cap plus margin.
		if len(text) > 8192 {
			text = text[:8192]
		}
		m := preview(0, text, start, end)
		if m.PreStart < 0 || m.PreEnd < m.PreStart || m.PreEnd > len(m.Preview) {
			t.Fatalf("preview coordinates outside its own text: %q %d %d", m.Preview, m.PreStart, m.PreEnd)
		}
		if len(m.Preview) > max(previewWidth+8+6, len(text)+6) {
			t.Fatalf("preview of %d bytes from a %d-byte line escapes its window", len(m.Preview), len(text))
		}
		if utf8.ValidString(text) && !utf8.ValidString(m.Preview) {
			// ripgrep can report lines that are not UTF-8, and those can
			// never preview cleanly; but a valid line must never be cut
			// mid-character, the preview's whole reason to exist.
			t.Fatalf("preview cut mid-character: %q", m.Preview)
		}
	})
}

// FuzzUnflip exercises the index mapping between the flipped "name dir"
// presentation and the path. Indexes deliberately range far outside the
// string: whatever comes in, every hit the mapping emits must stay inside
// the path, because the UI highlights bytes at those positions.
func FuzzUnflip(f *testing.F) {
	f.Add("dir/file.go", []byte{0, 1, 2, 3, 8, 9, 12, 200})
	f.Add("file.go", []byte{0, 5, 7})
	f.Add("a/b/c/d.txt", []byte{255, 254, 253})
	f.Add("", []byte{0, 1})
	f.Fuzz(func(t *testing.T, path string, raw []byte) {
		if len(path) > 4096 {
			path = path[:4096]
		}
		idx := make([]int, len(raw))
		for i, b := range raw {
			idx[i] = int(b)
		}
		for _, hit := range unflip(path, idx) {
			if hit < 0 || hit >= len(path) {
				t.Fatalf("hit %d outside path %q (%d bytes) from %v", hit, path, len(path), idx)
			}
		}
	})
}

// FuzzFiles exercises the multi-term ranking pipeline over arbitrary path
// sets and queries. Whatever is matched, result count stays within the
// limit and hits stay inside their paths. The path budget is 512, so the
// pipeline runs single-sharded here — Files shards at 4097 paths and up;
// parallel/serial equivalence is the unit test
// TestLargeFileQueriesKeepOrderAndHits's job, not this target's.
func FuzzFiles(f *testing.F) {
	f.Add("a.go\ninternal/app/model.go\ndocs/MainPage.md\nmain_test.go", "main", 60)
	f.Add("x", "x x x x", 1)
	f.Add("你好/世界.go", "世界", 10)
	f.Add("a/b/c", "/", 0)
	f.Fuzz(func(t *testing.T, list, query string, limit int) {
		if limit < 0 {
			limit = -limit
		}
		if limit > 500 {
			limit = 500 // budget: the UI never asks for more than 60
		}
		paths := strings.Split(list, "\n")
		if len(paths) > 512 {
			paths = paths[:512] // budget: ranking work scales with the set
		}
		for _, s := range Files(paths, query, limit) {
			for _, hit := range s.Hits {
				if hit < 0 || hit >= len(s.Path) {
					t.Fatalf("hit %d outside path %q", hit, s.Path)
				}
			}
		}
	})
}

// FuzzReplaceMatches drives the staleness-checked replacement with
// structured input: the harness decodes operations into matches that point
// at real lines of the text, hashing some lines correctly and corrupting
// others, so both the accept path and every rejection path stay exercised.
// The oracle is behavioral: a successful no-op replacement returns the
// input unchanged, and nothing ever panics or writes outside the text.
func FuzzReplaceMatches(f *testing.F) {
	f.Add("seed\nseed more\n", []byte{0, 0, 0, 4, 1})
	f.Add("a\nb\nc\n", []byte{255, 255, 255, 255, 255})
	f.Add("", []byte{0})
	f.Add("no newlines at all", []byte{1, 2, 3})
	f.Fuzz(func(t *testing.T, text string, ops []byte) {
		if len(text) > 16384 {
			text = text[:16384] // budget: files are capped at 4 MiB, but fuzzing needs no more
		}
		if len(ops) > 512 {
			ops = ops[:512] // budget: operations per input
		}
		lines := strings.SplitAfter(text, "\n")
		var matches []Match
		for i := 0; i+4 < len(ops); i += 5 {
			lineIdx := int(ops[i]) % max(len(lines), 1)
			line := strings.TrimSuffix(strings.TrimSuffix(lines[lineIdx], "\n"), "\r")
			if line == "" {
				continue
			}
			col := int(ops[i+1]) % len(line)
			end := col + 1 + int(ops[i+2])%max(len(line)-col, 1)
			if end > len(line) {
				end = len(line)
			}
			var m Match
			m.Line = lineIdx
			m.Col, m.End = col, end
			m.lineHash = sha256.Sum256([]byte(line))
			if ops[i+3]%3 == 0 {
				m.lineHash[0] ^= 0xff // corrupt: the line must read as stale
			}
			matches = append(matches, m)
			if len(matches) >= 64 {
				break // budget: bounded replacement operations
			}
		}
		out, count, err := replaceMatches(text, matches, "R")
		if err == nil && count == 0 && out != text {
			t.Fatalf("a no-op replacement changed the text: %q -> %q", text, out)
		}
		if err == nil && count > 0 {
			growth := len(out) - len(text)
			if growth > len(matches) { // "R" is one byte; a match replaces at least one byte
				t.Fatalf("output grew by %d, beyond what the matches explain", growth)
			}
		}
	})
}
