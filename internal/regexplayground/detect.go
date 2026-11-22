package regexplayground

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Literal is a recognizable regular expression in source code.
type Literal struct {
	Pattern          string
	Flags            string
	Start, End       int
	UnsupportedFlags []string
}

// Detect finds a regular-expression literal containing offset in source.
// It recognizes regexp.Compile/MustCompile calls in Go and slash literals in
// JavaScript-family files.
func Detect(source, path string, offset int) (Literal, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return detectGo(source, offset)
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":
		return detectJS(source, offset)
	default:
		return Literal{}, false
	}
}

func IsJavaScriptPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":
		return true
	default:
		return false
	}
}

// UnsupportedJSFeatures lists constructs that Go's RE2 engine cannot
// evaluate with JavaScript semantics, including constructs RE2 accepts but
// JavaScript does not.
func UnsupportedJSFeatures(pattern string) []string {
	features := make(map[string]bool)
	inClass := false
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			if i+1 < len(pattern) {
				next := pattern[i+1]
				switch {
				case next >= '1' && next <= '9', next == 'k':
					features["backreferences"] = true
				case next == 'p' || next == 'P':
					features["Unicode property escapes"] = true
				case next == 'A' || next == 'z' || next == 'Q' || next == 'E':
					features["Go-only escapes"] = true
				}
				i++
			}
			continue
		}
		if pattern[i] == '[' {
			inClass = true
			continue
		}
		if pattern[i] == ']' {
			inClass = false
			continue
		}
		if inClass || pattern[i] != '(' || i+2 >= len(pattern) || pattern[i+1] != '?' {
			continue
		}
		switch pattern[i+2] {
		case '=', '!':
			features["lookaround"] = true
		case '<':
			if i+3 < len(pattern) && (pattern[i+3] == '=' || pattern[i+3] == '!') {
				features["lookaround"] = true
			} else {
				features["JavaScript named groups"] = true
			}
		case 'P':
			features["Go-only named groups"] = true
		case '>':
			features["atomic groups"] = true
		case '(':
			features["conditional groups"] = true
		case 'i', 'm', 's', 'U', '-':
			features["inline mode flags"] = true
		}
	}
	out := make([]string, 0, len(features))
	for feature := range features {
		out = append(out, feature)
	}
	sort.Strings(out)
	return out
}

func detectGo(source string, offset int) (Literal, bool) {
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "source.go", source, parser.AllErrors|parser.ParseComments)
	if file == nil {
		return Literal{}, false
	}
	var found Literal
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Compile" && selector.Sel.Name != "MustCompile") {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "regexp" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		start := fset.Position(literal.Pos()).Offset
		end := fset.Position(literal.End()).Offset
		if offset < start || offset > end {
			return true
		}
		pattern, err := strconv.Unquote(literal.Value)
		if err == nil {
			found = Literal{Pattern: pattern, Start: start, End: end}
		}
		return true
	})
	return found, found.End != 0
}

func detectJS(source string, offset int) (Literal, bool) {
	var candidates []Literal
	expectExpression := true
	for i := 0; i < len(source); {
		c := source[i]
		if isSpace(c) {
			i++
			continue
		}
		if c == '/' && i+1 < len(source) && source[i+1] == '/' {
			i += 2
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(source) && source[i+1] == '*' {
			i += 2
			for i+1 < len(source) && source[i:i+2] != "*/" {
				i++
			}
			if i+1 < len(source) {
				i += 2
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			i = skipQuoted(source, i, c)
			expectExpression = false
			continue
		}
		if c == '/' {
			if expectExpression {
				if literal, end, ok := jsLiteral(source, i); ok {
					candidates = append(candidates, literal)
					i = end
					expectExpression = false
					continue
				}
			}
			i++
			expectExpression = true
			continue
		}
		if isIdentifierStart(c) {
			start := i
			i++
			for i < len(source) && isIdentifierPart(source[i]) {
				i++
			}
			word := source[start:i]
			expectExpression = jsPrefixKeyword(word)
			continue
		}
		if c >= '0' && c <= '9' {
			i++
			for i < len(source) && (isIdentifierPart(source[i]) || source[i] == '.') {
				i++
			}
			expectExpression = false
			continue
		}
		switch c {
		case ')', ']', '}':
			expectExpression = false
		case '.', '?':
			expectExpression = false
		case '(', '[', '{', ',', ':', ';', '=', '+', '-', '*', '%', '&', '|', '^', '!', '~', '<', '>':
			expectExpression = true
		default:
			expectExpression = true
		}
		i++
	}
	for _, candidate := range candidates {
		if offset >= candidate.Start && offset <= candidate.End {
			return candidate, true
		}
	}
	return Literal{}, false
}

func jsLiteral(source string, start int) (Literal, int, bool) {
	inClass := false
	for i := start + 1; i < len(source); i++ {
		switch source[i] {
		case '\n', '\r':
			return Literal{}, start + 1, false
		case '\\':
			i++
		case '[':
			if !inClass {
				inClass = true
			}
		case ']':
			inClass = false
		case '/':
			if inClass {
				continue
			}
			end := i + 1
			for end < len(source) && isIdentifierPart(source[end]) {
				end++
			}
			flags := source[i+1 : end]
			unsupported := unsupportedJSFlags(flags)
			pattern := source[start+1 : i]
			return Literal{Pattern: pattern, Flags: flags, Start: start, End: end, UnsupportedFlags: unsupported}, end, true
		}
	}
	return Literal{}, start + 1, false
}

func unsupportedJSFlags(flags string) []string {
	seen := make(map[byte]bool)
	unsupported := make(map[string]bool)
	for i := 0; i < len(flags); i++ {
		flag := flags[i]
		if seen[flag] || !strings.ContainsRune("gimsuydv", rune(flag)) || !strings.ContainsRune("gims", rune(flag)) {
			unsupported[string(flag)] = true
		}
		seen[flag] = true
	}
	out := make([]string, 0, len(unsupported))
	for flag := range unsupported {
		out = append(out, flag)
	}
	sort.Strings(out)
	return out
}

func skipQuoted(source string, start int, quote byte) int {
	for i := start + 1; i < len(source); i++ {
		if source[i] == '\\' {
			i++
			continue
		}
		if source[i] == quote {
			return i + 1
		}
		if quote != '`' && (source[i] == '\n' || source[i] == '\r') {
			return i
		}
	}
	return len(source)
}

func jsPrefixKeyword(word string) bool {
	switch word {
	case "await", "case", "delete", "do", "else", "in", "instanceof", "new", "of", "return", "throw", "typeof", "void", "yield":
		return true
	default:
		return false
	}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func isIdentifierStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentifierPart(c byte) bool { return isIdentifierStart(c) || c >= '0' && c <= '9' }
