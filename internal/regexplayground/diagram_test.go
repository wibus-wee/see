package regexplayground

import (
	"strings"
	"testing"
)

func TestParseDiagramCapturesAlternativesAndRepetitions(t *testing.T) {
	d, err := ParseDiagram(`(?P<word>\p{L}+)([0-9]+)?|cat{2,4}`, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	var captures []DiagramNode
	var repeats []string
	var alternatives int
	walkDiagram(d.Root, func(n DiagramNode) {
		switch n.Kind {
		case DiagramCapture:
			captures = append(captures, n)
		case DiagramRepeat:
			repeats = append(repeats, n.Label)
		case DiagramAlternate:
			alternatives++
		}
	})
	if len(captures) != 2 || captures[0].Capture != 1 || captures[0].Name != "word" || captures[1].Capture != 2 {
		t.Fatalf("captures = %+v", captures)
	}
	for _, want := range []string{"+", "?", "{2,4}"} {
		if !containsString(repeats, want) {
			t.Errorf("repetition %q missing from %v", want, repeats)
		}
	}
	if alternatives != 1 {
		t.Fatalf("alternation nodes = %d, want 1", alternatives)
	}
}

func TestParseDiagramUsesFlagsAndRejectsInvalidPatterns(t *testing.T) {
	for _, flags := range []Flags{{IgnoreCase: true}, {Multiline: true}, {DotAll: true}} {
		if _, err := ParseDiagram(".", flags); err != nil {
			t.Fatalf("ParseDiagram with flags %+v: %v", flags, err)
		}
	}
	if _, err := ParseDiagram("(", Flags{}); err == nil {
		t.Fatal("invalid pattern produced a diagram")
	}
	for _, pattern := range []string{"(?=a)", `\k<name>`, `\1`} {
		if _, err := ParseDiagram(pattern, Flags{}); err == nil {
			t.Errorf("unsupported RE2 pattern %q produced a diagram", pattern)
		}
	}
}

func TestParseDiagramKeepsUnicodeClassesCompact(t *testing.T) {
	d, err := ParseDiagram(`(?P<word>\p{L}+)([0-9]+)?`, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Abbreviated {
		t.Fatal("ordinary Unicode class pattern was unexpectedly abbreviated")
	}
	var captures []DiagramNode
	walkDiagram(d.Root, func(n DiagramNode) {
		if n.Kind == DiagramCapture {
			captures = append(captures, n)
		}
	})
	if len(captures) != 2 {
		t.Fatalf("captures = %+v, want both named and optional groups", captures)
	}
	if class := firstDiagramClass(d.Root); class != DiagramClassLetter {
		t.Fatalf("Unicode letter class = %v, want DiagramClassLetter", class)
	}
}

func TestParseDiagramLabelsCommonAndCanonicalClasses(t *testing.T) {
	for _, test := range []struct {
		pattern string
		class   DiagramClass
	}{
		{`\p{L}`, DiagramClassLetter},
		{`[0-9]`, DiagramClassDigit},
		{`\d`, DiagramClassDigit},
		{`\s`, DiagramClassWhitespace},
		{`\w`, DiagramClassWord},
		{`\D`, DiagramClassNonDigit},
		{`\S`, DiagramClassNonWhitespace},
		{`\W`, DiagramClassNonWord},
	} {
		d, err := ParseDiagram(test.pattern, Flags{})
		if err != nil {
			t.Fatalf("ParseDiagram(%q): %v", test.pattern, err)
		}
		if got := firstDiagramClass(d.Root); got != test.class {
			t.Errorf("ParseDiagram(%q) class = %v, want %v", test.pattern, got, test.class)
		}
		if d.Abbreviated {
			t.Errorf("common class %q was unexpectedly abbreviated", test.pattern)
		}
	}

	d, err := ParseDiagram(`[a-z]`, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if got := firstDiagramNode(d.Root, DiagramTerminal).Label; got != `[a-z]` {
		t.Fatalf("canonical class label = %q, want [a-z]", got)
	}

	d, err = ParseDiagram(`\p{Greek}`, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	label := firstDiagramNode(d.Root, DiagramTerminal).Label
	if !d.Abbreviated || len([]rune(label)) > maxDiagramLabelRunes || !strings.HasPrefix(label, "[") || !strings.HasSuffix(label, "…]") {
		t.Fatalf("large canonical class label = %q, abbreviated %v", label, d.Abbreviated)
	}
}

func TestParseDiagramPreservesLazyAndBoundedRepeatSemantics(t *testing.T) {
	d, err := ParseDiagram(`a+?b{2,4}?c?`, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	var repeats []DiagramNode
	walkDiagram(d.Root, func(n DiagramNode) {
		if n.Kind == DiagramRepeat {
			repeats = append(repeats, n)
		}
	})
	if len(repeats) != 3 {
		t.Fatalf("repeats = %+v, want three", repeats)
	}
	if !repeats[0].Lazy || repeats[0].RepeatMin != 1 || repeats[0].RepeatMax != -1 || repeats[0].Label != "+?" {
		t.Errorf("lazy plus = %+v", repeats[0])
	}
	if !repeats[1].Lazy || repeats[1].RepeatMin != 2 || repeats[1].RepeatMax != 4 || repeats[1].Label != "{2,4}?" {
		t.Errorf("lazy bounded repeat = %+v", repeats[1])
	}
	if repeats[2].Lazy || repeats[2].RepeatMin != 0 || repeats[2].RepeatMax != 1 {
		t.Errorf("optional repeat = %+v", repeats[2])
	}
}

func TestParseDiagramAbbreviatesWithinDepthAndNodeLimits(t *testing.T) {
	pattern := strings.Repeat("(", 20) + "a" + strings.Repeat(")", 20)
	d, err := ParseDiagram(pattern, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Abbreviated || d.NodeCount > MaxDiagramNodes {
		t.Fatalf("deep diagram = abbreviated %v, nodes %d", d.Abbreviated, d.NodeCount)
	}

	pattern = strings.Repeat("(a)|", MaxDiagramNodes) + "(z)"
	d, err = ParseDiagram(pattern, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Abbreviated || d.NodeCount > MaxDiagramNodes {
		t.Fatalf("wide diagram = abbreviated %v, nodes %d", d.Abbreviated, d.NodeCount)
	}
	if got := len(d.Root.Children); got > MaxDiagramAlternatives+1 {
		t.Fatalf("wide alternation has %d visible branches, want at most %d plus ellipsis", got, MaxDiagramAlternatives)
	}

	d, err = ParseDiagram(strings.Repeat("a", maxDiagramLabelRunes+4), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !d.Abbreviated || !strings.HasSuffix(d.Root.Label, "…") {
		t.Fatalf("long terminal = %+v, abbreviated %v", d.Root, d.Abbreviated)
	}
}

func TestCaptureHighlightsUseUnicodeByteRangesAndInnermostWins(t *testing.T) {
	spans := CaptureHighlights([]Capture{
		{Number: 0, Start: 0, End: 5, Matched: true},
		{Number: 1, Start: 0, End: 5, Matched: true},
		{Number: 2, Start: 3, End: 5, Matched: true},
		{Number: 3, Start: 4, End: 4, Matched: true},
		{Number: 4, Start: 1, End: 3},
	})
	want := []CaptureSpan{{Start: 0, End: 3, Number: 1}, {Start: 3, End: 5, Number: 2}}
	if len(spans) != len(want) {
		t.Fatalf("spans = %+v, want %+v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}
}

func TestCaptureHighlightsBreakEqualSizeTiesByGroupNumber(t *testing.T) {
	spans := CaptureHighlights([]Capture{
		{Number: 2, Start: 2, End: 6, Matched: true},
		{Number: 1, Start: 0, End: 4, Matched: true},
	})
	want := []CaptureSpan{{Start: 0, End: 4, Number: 1}, {Start: 4, End: 6, Number: 2}}
	if len(spans) != len(want) {
		t.Fatalf("spans = %+v, want %+v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}
}

func walkDiagram(n DiagramNode, visit func(DiagramNode)) {
	visit(n)
	for _, child := range n.Children {
		walkDiagram(child, visit)
	}
}

func firstDiagramClass(n DiagramNode) DiagramClass {
	if n.Class != DiagramClassNone {
		return n.Class
	}
	for _, child := range n.Children {
		if class := firstDiagramClass(child); class != DiagramClassNone {
			return class
		}
	}
	return DiagramClassNone
}

func firstDiagramNode(n DiagramNode, kind DiagramKind) DiagramNode {
	if n.Kind == kind {
		return n
	}
	for _, child := range n.Children {
		if found := firstDiagramNode(child, kind); found.Kind == kind {
			return found
		}
	}
	return DiagramNode{}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
