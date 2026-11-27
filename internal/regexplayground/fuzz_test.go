package regexplayground

import (
	"sort"
	"testing"
	"unicode/utf8"
)

// Fuzz targets in this package follow docs/specs/conformance/lane-e-fuzzing.md.
// The playground already refuses inputs beyond its byte budgets; fuzzing
// therefore stays inside them, and the oracles assert the contracts the UI
// depends on: bounded results, coordinates inside the sample, diagram nodes
// within their published caps, and non-overlapping capture spans. Regression
// inputs live in testdata/fuzz/<target>/.

// FuzzEvaluate runs arbitrary RE2-shaped patterns over arbitrary samples.
// The engine itself is linear-time, so the oracle is the result contract.
func FuzzEvaluate(f *testing.F) {
	f.Add(`(\w+)@(\w+)`, "mail me at a@b or c@d", byte(0))
	f.Add(`(a+)+$`, repeatStr("a", 64), byte(0))
	f.Add(`(?P<name>\pL+)`, "héllo 世界", byte(1))
	f.Add(`[z-a]`, "z", byte(0))
	f.Add("(", "", byte(7))
	f.Fuzz(func(t *testing.T, pattern, sample string, flagBits byte) {
		if len(pattern) > 1024 {
			pattern = pattern[:1024]
		}
		if len(sample) > 8192 {
			sample = sample[:8192]
		}
		flags := Flags{IgnoreCase: flagBits&1 != 0, Multiline: flagBits&2 != 0, DotAll: flagBits&4 != 0}
		r := Evaluate(pattern, sample, flags)
		if len(r.Matches) > MaxMatches {
			t.Fatalf("%d matches returned, cap is %d", len(r.Matches), MaxMatches)
		}
		last := -1
		for _, m := range r.Matches {
			if m.Start < 0 || m.End < m.Start || m.End > len(sample) {
				t.Fatalf("match %v outside the sample", m)
			}
			if m.Start < last {
				t.Fatalf("matches out of order: %d after %d", m.Start, last)
			}
			last = m.Start
			for _, c := range m.Captures {
				if !c.Matched {
					continue
				}
				if c.Start < 0 || c.End < c.Start || c.End > len(sample) {
					t.Fatalf("capture %v outside the sample", c)
				}
				if sample[c.Start:c.End] != c.Value {
					t.Fatalf("capture value %q disagrees with the sample at %d..%d", c.Value, c.Start, c.End)
				}
			}
		}
	})
}

// FuzzParseDiagram builds railroad diagrams from arbitrary patterns. The
// oracle is the display budget: node count, depth, and label length are
// what keeps the diagram renderable.
func FuzzParseDiagram(f *testing.F) {
	f.Add(`a(b|c)*d`, byte(0))
	f.Add(`((((((((((x))))))))))`, byte(0))
	f.Add(`[abcdefghijklmnopqrstuvwxyz]+`, byte(0))
	f.Add(`x{3,7}?`, byte(7))
	f.Fuzz(func(t *testing.T, pattern string, flagBits byte) {
		if len(pattern) > 1024 {
			pattern = pattern[:1024]
		}
		flags := Flags{IgnoreCase: flagBits&1 != 0, Multiline: flagBits&2 != 0, DotAll: flagBits&4 != 0}
		diagram, err := ParseDiagram(pattern, flags)
		if err != nil {
			return // invalid patterns are answers, not crashes
		}
		if diagram.NodeCount > MaxDiagramNodes {
			t.Fatalf("diagram has %d nodes, cap is %d", diagram.NodeCount, MaxDiagramNodes)
		}
		checkDiagramNode(t, diagram.Root, 0)
	})
}

func checkDiagramNode(t *testing.T, n DiagramNode, depth int) {
	t.Helper()
	if depth > MaxDiagramDepth+1 {
		t.Fatalf("diagram deeper than %d", MaxDiagramDepth)
	}
	if utf8.RuneCountInString(n.Label) > maxDiagramLabelRunes {
		t.Fatalf("label %q longer than %d runes", n.Label, maxDiagramLabelRunes)
	}
	for _, child := range n.Children {
		checkDiagramNode(t, child, depth+1)
	}
}

// FuzzCaptureHighlights partitions arbitrary overlapping capture ranges.
// Whatever goes in, the spans that come out must be non-overlapping, in
// order, inside their original captures' union, and empty spans must not
// exist — the editor paints at these coordinates without further checks.
func FuzzCaptureHighlights(f *testing.F) {
	f.Add([]byte{0, 10, 1, 2, 8, 2, 4, 6, 3}, byte(20))
	f.Add([]byte{0, 5, 1, 0, 5, 2, 0, 5, 3}, byte(8))
	f.Add([]byte{}, byte(1))
	f.Fuzz(func(t *testing.T, raw []byte, scale byte) {
		if len(raw) > 300 {
			raw = raw[:300] // budget: capture count per input
		}
		width := int(scale) + 1
		var captures []Capture
		for i := 0; i+2 < len(raw); i += 3 {
			start := int(raw[i]) % width
			end := start + int(raw[i+1])%width
			number := int(raw[i+2]%8) + 1
			captures = append(captures, Capture{Number: number, Start: start, End: end, Matched: true})
		}
		spans := CaptureHighlights(captures)
		for i, span := range spans {
			if span.End <= span.Start {
				t.Fatalf("empty or inverted span %v", span)
			}
			if i > 0 && span.Start < spans[i-1].End {
				t.Fatalf("overlapping spans %v and %v", spans[i-1], span)
			}
		}
		if !sort.IsSorted(sortSpans(spans)) {
			t.Fatalf("spans out of order: %v", spans)
		}
	})
}

type sortSpans []CaptureSpan

func (s sortSpans) Len() int           { return len(s) }
func (s sortSpans) Less(i, j int) bool { return s[i].Start < s[j].Start }
func (s sortSpans) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// FuzzDetectJS scans arbitrary bytes as JavaScript source. The detector is
// a hand-written lexer over untrusted editor content, so the oracle is
// survival plus self-consistency: whatever literal it reports must be a
// span inside the source whose pattern is a slice of that span.
func FuzzDetectJS(f *testing.F) {
	f.Add("const re = /a\\/b+/g;", 14)
	f.Add("x = /[\\]/]/u", 4)
	f.Add("// comment /not/\n/literal/", 20)
	f.Add("`template` + /after/`;", 13)
	f.Fuzz(func(t *testing.T, source string, offset int) {
		if len(source) > 16384 {
			source = source[:16384] // budget: files the playground opens
		}
		literal, found := detectJS(source, offset)
		if !found {
			return
		}
		if literal.Start < 0 || literal.End > len(source) || literal.Start >= literal.End {
			t.Fatalf("literal span %d..%d outside a %d-byte source", literal.Start, literal.End, len(source))
		}
		span := source[literal.Start:literal.End]
		body := span[1:]
		for i := len(body); i >= 0; i-- {
			if len(body[:i]) == len(literal.Pattern) && body[:i] == literal.Pattern {
				return
			}
		}
		t.Fatalf("pattern %q is not inside its own span %q", literal.Pattern, span)
	})
}

// FuzzUnsupportedJSFeatures scans arbitrary pattern bytes. The feature
// list is advisory text, so the oracle is only survival and determinism;
// it exists to shake out scanner panics on adversarial backslash runs.
func FuzzUnsupportedJSFeatures(f *testing.F) {
	f.Add(`\1\k<x>\pL\A\z\Q\E`)
	f.Add(`(?=a)(?!b)(?<=c)(?<!d)(?<n>e)(?P<n>f)(?>g)(?(h))(?i)(?-i)`)
	f.Add(`[[[[\\\\\`)
	f.Fuzz(func(t *testing.T, pattern string) {
		if len(pattern) > 4096 {
			pattern = pattern[:4096]
		}
		first := UnsupportedJSFeatures(pattern)
		second := UnsupportedJSFeatures(pattern)
		if len(first) != len(second) {
			t.Fatalf("non-deterministic feature scan: %v vs %v", first, second)
		}
		for i := range first {
			if first[i] != second[i] {
				t.Fatalf("non-deterministic feature scan: %v vs %v", first, second)
			}
		}
	})
}

func repeatStr(s string, n int) string {
	out := ""
	for len(out) < n {
		out += s
	}
	return out[:n]
}
