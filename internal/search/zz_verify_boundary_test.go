package search

// Independent verifier's boundary probes — NOT part of Lane E's implementation.
// These sweep preview() and unflip() over structured boundary values that
// random fuzzing is unlikely to hit exactly: multi-byte characters placed at
// the window edges, start/end straddling character boundaries, widths at and
// around previewWidth.

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func verifyPreviewOracle(t *testing.T, text string, start, end int) {
	t.Helper()
	m := preview(0, text, start, end)
	if m.PreStart < 0 || m.PreEnd < m.PreStart || m.PreEnd > len(m.Preview) {
		t.Errorf("text=%q start=%d end=%d: coords outside preview: PreStart=%d PreEnd=%d len=%d",
			text[:min(len(text), 60)], start, end, m.PreStart, m.PreEnd, len(m.Preview))
	}
	if utf8.ValidString(text) && !utf8.ValidString(m.Preview) {
		t.Errorf("text len=%d start=%d end=%d: valid input cut mid-character", len(text), start, end)
	}
	// Stronger oracle than the fuzz one: for IN-CONTRACT input (valid UTF-8,
	// coordinates at character boundaries), the highlight span must cover the
	// whole match — the window must not cut the match off.
	if utf8.ValidString(text) && start >= 0 && end <= len(text) && start < end &&
		utf8.RuneStart(text[start]) && (end == len(text) || utf8.RuneStart(text[end])) {
		cs, ce := clampI(start, 0, len(text)), clampI(end, 0, len(text))
		if m.PreEnd-m.PreStart < ce-cs {
			t.Errorf("text len=%d start=%d end=%d: highlight %d..%d covers %d of %d match bytes — match cut by window",
				len(text), start, end, m.PreStart, m.PreEnd, m.PreEnd-m.PreStart, ce-cs)
		}
	}
}

func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func TestVerifyPreviewBoundarySweep(t *testing.T) {
	// Lines of varying widths around previewWidth=200, with multi-byte
	// characters at every position class.
	cjk := strings.Repeat("世", 80)  // 240 bytes, all 3-byte runes
	mixed := strings.Repeat("a", 97) + "世界" + strings.Repeat("b", 97) // 200 bytes-ish, multibyte at 97..103
	long := strings.Repeat("x", 500)
	spacey := strings.Repeat(" ", 30) + strings.Repeat("w", 300)
	lines := []string{cjk, mixed, long, spacey, strings.Repeat("é", 120)}
	for li, text := range lines {
		// positions of interest: exact multiples and ±1..±4 around window edges
		var positions []int
		for base := 0; base <= len(text); base += 1 {
			positions = append(positions, base)
		}
		if len(positions) > 4000 {
			positions = positions[:4000]
		}
		for _, start := range positions {
			for _, delta := range []int{1, 2, 3, 4, 7, 40, 100, 199, 200, 201, 300} {
				end := start + delta
				verifyPreviewOracle(t, text, start, end)
				// out-of-contract variants
				verifyPreviewOracle(t, text, start, end+5)
				verifyPreviewOracle(t, text, start-3, end)
				verifyPreviewOracle(t, text, start, -end)
			}
		}
		if li == 0 {
			t.Log("cjk line swept")
		}
	}
}

func TestVerifyUnflipBoundarySweep(t *testing.T) {
	paths := []string{
		"a.go",
		"d/f.go",
		"very/deeply/nested/dir/structure/file_name_test.go",
		"世界/文件.go",
		"/leading.go",
		"trailing/",
		"",
		"nodir",
	}
	for _, p := range paths {
		flipped := len(p) + 1
		for i := -2; i <= flipped+2; i++ {
			for _, hit := range unflip(p, []int{i}) {
				if hit < 0 || hit >= len(p) {
					t.Errorf("path %q idx %d -> hit %d outside path (%d bytes)", p, i, hit, len(p))
				}
			}
		}
		// exhaustive small index sets
		for i := 0; i < flipped+1 && i < 40; i++ {
			for j := i; j < flipped+1 && j < 40; j++ {
				for _, hit := range unflip(p, []int{i, j}) {
					if hit < 0 || hit >= len(p) {
						t.Errorf("path %q idx [%d %d] -> hit %d outside", p, i, j, hit)
					}
				}
			}
		}
	}
}

// The CRLF seam: trimLineEnding removes \r\n but ripgrep's coordinates count
// the \r. A match ending exactly at the trimmed \r boundary goes through the
// real pipeline semantics.
func TestVerifyPreviewCRLFSeam(t *testing.T) {
	// line as rg reports it in JSON: includes trailing \r\n; the parser trims.
	raw := "alpha beta gamma\r\n"
	line := trimLineEnding(raw)
	if line != "alpha beta gamma" {
		t.Fatalf("trimLineEnding = %q", line)
	}
	// a match whose end coordinate (rg-counted, includes \r) exceeds the trimmed line
	for _, tc := range [][2]int{{0, 5}, {15, 16}, {16, 17}, {15, 17}, {0, 18}} {
		verifyPreviewOracle(t, line, tc[0], tc[1])
	}
	fmt.Println("crlf seam swept")
}
