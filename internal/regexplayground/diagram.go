package regexplayground

import (
	"fmt"
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

const (
	MaxDiagramNodes        = 32
	MaxDiagramDepth        = 8
	MaxDiagramAlternatives = 5
	maxDiagramLabelRunes   = 18
)

type DiagramKind uint8

const (
	DiagramTerminal DiagramKind = iota
	DiagramEmpty
	DiagramSequence
	DiagramAlternate
	DiagramCapture
	DiagramRepeat
	DiagramEllipsis
)

type DiagramClass uint8

const (
	DiagramClassNone DiagramClass = iota
	DiagramClassLetter
	DiagramClassDigit
	DiagramClassWhitespace
	DiagramClassWord
	DiagramClassNonDigit
	DiagramClassNonWhitespace
	DiagramClassNonWord
)

type DiagramNode struct {
	Kind      DiagramKind
	Label     string
	Capture   int
	Name      string
	RepeatMin int
	RepeatMax int
	Class     DiagramClass
	Lazy      bool
	Children  []DiagramNode
}

type Diagram struct {
	Root        DiagramNode
	NodeCount   int
	Abbreviated bool
}

func ParseDiagram(pattern string, flags Flags) (Diagram, error) {
	if len(pattern) > MaxPatternBytes {
		return Diagram{}, fmt.Errorf("pattern exceeds %d bytes", MaxPatternBytes)
	}
	re, err := syntax.Parse(withFlags(pattern, flags), syntax.Perl)
	if err != nil {
		return Diagram{}, err
	}
	b := diagramBuilder{}
	root := b.build(re, 0)
	return Diagram{Root: root, NodeCount: b.nodes, Abbreviated: b.abbreviated}, nil
}

type diagramBuilder struct {
	nodes       int
	abbreviated bool
}

func (b *diagramBuilder) build(re *syntax.Regexp, depth int) DiagramNode {
	if b.abbreviated {
		return DiagramNode{Kind: DiagramEllipsis}
	}
	if depth >= MaxDiagramDepth || b.nodes >= MaxDiagramNodes-1 {
		b.nodes++
		b.abbreviated = true
		return DiagramNode{Kind: DiagramEllipsis}
	}
	b.nodes++
	n := DiagramNode{}
	switch re.Op {
	case syntax.OpNoMatch:
		n.Kind, n.Label = DiagramTerminal, "∅"
	case syntax.OpEmptyMatch:
		n.Kind = DiagramEmpty
	case syntax.OpConcat:
		n.Kind = DiagramSequence
		n.Children = b.children(re.Sub, depth)
	case syntax.OpAlternate:
		n.Kind = DiagramAlternate
		n.Children = b.alternatives(re.Sub, depth)
	case syntax.OpCapture:
		n.Kind, n.Capture, n.Name = DiagramCapture, re.Cap, re.Name
		if utf8.RuneCountInString(n.Name) > maxDiagramLabelRunes {
			b.abbreviated = true
		}
		n.Children = b.children(re.Sub, depth)
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		n.Kind, n.Label = DiagramRepeat, repeatLabel(re)
		n.RepeatMin, n.RepeatMax = repeatBounds(re)
		n.Lazy = re.Flags&syntax.NonGreedy != 0
		n.Children = b.children(re.Sub, depth)
	case syntax.OpCharClass:
		n.Kind = DiagramTerminal
		n.Class = diagramClass(re)
		if n.Class == DiagramClassNone {
			n.Label = b.classLabel(re.String())
		}
	default:
		n.Kind, n.Label = DiagramTerminal, b.label(re.String())
	}
	return n
}

func (b *diagramBuilder) classLabel(label string) string {
	runes := []rune(label)
	if len(runes) <= maxDiagramLabelRunes {
		return label
	}
	b.abbreviated = true
	if strings.HasPrefix(label, "[") && strings.HasSuffix(label, "]") {
		body := label[1 : len(label)-1]
		var prefix strings.Builder
		count := 0
		for _, token := range classTokens(body) {
			tokenRunes := utf8.RuneCountInString(token)
			if count+tokenRunes > maxDiagramLabelRunes-3 {
				break
			}
			prefix.WriteString(token)
			count += tokenRunes
		}
		return "[" + prefix.String() + "…]"
	}
	return string(runes[:maxDiagramLabelRunes-1]) + "…"
}

func classTokens(class string) []string {
	var tokens []string
	for i := 0; i < len(class); {
		start := i
		if class[i] == '\\' {
			i++
			if i < len(class) && class[i] == 'x' && i+1 < len(class) && class[i+1] == '{' {
				if end := strings.IndexByte(class[i+2:], '}'); end >= 0 {
					i += end + 3
				} else {
					i = len(class)
				}
			} else if i < len(class) {
				_, size := utf8.DecodeRuneInString(class[i:])
				i += size
			}
		} else {
			_, size := utf8.DecodeRuneInString(class[i:])
			i += size
		}
		tokens = append(tokens, class[start:i])
	}
	return tokens
}

var knownDiagramClasses = []struct {
	class DiagramClass
	runes []rune
}{
	{DiagramClassLetter, syntaxClassRunes(`\p{L}`)},
	{DiagramClassDigit, syntaxClassRunes(`[0-9]`)},
	{DiagramClassWhitespace, syntaxClassRunes(`\s`)},
	{DiagramClassWord, syntaxClassRunes(`\w`)},
	{DiagramClassNonDigit, syntaxClassRunes(`\D`)},
	{DiagramClassNonWhitespace, syntaxClassRunes(`\S`)},
	{DiagramClassNonWord, syntaxClassRunes(`\W`)},
}

func syntaxClassRunes(pattern string) []rune {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil || re.Op != syntax.OpCharClass {
		return nil
	}
	return re.Rune
}

func diagramClass(re *syntax.Regexp) DiagramClass {
	for _, known := range knownDiagramClasses {
		if len(re.Rune) != len(known.runes) {
			continue
		}
		match := true
		for i := range re.Rune {
			if re.Rune[i] != known.runes[i] {
				match = false
				break
			}
		}
		if match {
			return known.class
		}
	}
	return DiagramClassNone
}

func (b *diagramBuilder) alternatives(sub []*syntax.Regexp, depth int) []DiagramNode {
	children := make([]DiagramNode, 0, min(len(sub), MaxDiagramAlternatives)+1)
	for i, re := range sub {
		if i == MaxDiagramAlternatives || b.abbreviated {
			if b.nodes < MaxDiagramNodes {
				b.nodes++
				children = append(children, DiagramNode{Kind: DiagramEllipsis})
			}
			b.abbreviated = true
			break
		}
		children = append(children, b.build(re, depth+1))
	}
	return children
}

func (b *diagramBuilder) label(label string) string {
	runes := []rune(label)
	if len(runes) <= maxDiagramLabelRunes {
		return label
	}
	b.abbreviated = true
	return string(runes[:maxDiagramLabelRunes-1]) + "…"
}

func repeatBounds(re *syntax.Regexp) (int, int) {
	switch re.Op {
	case syntax.OpStar:
		return 0, -1
	case syntax.OpPlus:
		return 1, -1
	case syntax.OpQuest:
		return 0, 1
	default:
		return re.Min, re.Max
	}
}

func (b *diagramBuilder) children(sub []*syntax.Regexp, depth int) []DiagramNode {
	children := make([]DiagramNode, 0, len(sub))
	for _, re := range sub {
		if b.abbreviated {
			if b.nodes < MaxDiagramNodes {
				b.nodes++
				children = append(children, DiagramNode{Kind: DiagramEllipsis})
			}
			break
		}
		children = append(children, b.build(re, depth+1))
	}
	return children
}

func repeatLabel(re *syntax.Regexp) string {
	label := ""
	switch re.Op {
	case syntax.OpStar:
		label = "*"
	case syntax.OpPlus:
		label = "+"
	case syntax.OpQuest:
		label = "?"
	case syntax.OpRepeat:
		switch {
		case re.Max < 0:
			label = fmt.Sprintf("{%d,}", re.Min)
		case re.Min == re.Max:
			label = fmt.Sprintf("{%d}", re.Min)
		default:
			label = fmt.Sprintf("{%d,%d}", re.Min, re.Max)
		}
	}
	if re.Flags&syntax.NonGreedy != 0 {
		label += "?"
	}
	return label
}
