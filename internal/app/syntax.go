package app

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/editor"
)

var cmakeLanguage = &editor.Language{
	Name: "CMake", Extensions: []string{".cmake"}, Filenames: []string{"CMakeLists.txt"}, LineComment: "#", TabSize: 2,
	Brackets:       []editor.BracketPair{{Open: "(", Close: ")"}, {Open: "{", Close: "}"}},
	AutoClose:      []editor.BracketPair{{Open: "(", Close: ")"}, {Open: "\"", Close: "\"", NotIn: []editor.TokenKind{editor.TokenString, editor.TokenComment}}},
	IncreaseIndent: regexp.MustCompile(`(?i)^\s*(if|foreach|while|function|macro|block)\s*\(`),
	DecreaseIndent: regexp.MustCompile(`(?i)^\s*(endif|endforeach|endwhile|endfunction|endmacro|endblock|else|elseif)\s*\(`),
	Tokenizer:      editor.TokenizerFunc(cmakeTokenize),
}

func languageFor(name, text string) *editor.Language {
	if filepath.Base(name) == "CMakeLists.txt" || strings.EqualFold(filepath.Ext(name), ".cmake") {
		return cmakeLanguage
	}
	return editor.LanguageForText(name, text)
}

// CMake bracket arguments and comments carry their delimiter length across lines.
func cmakeTokenize(line string, state uint64, tokens []editor.Token) ([]editor.Token, uint64) {
	emit := func(end int, kind editor.TokenKind) {
		if end > 0 && (len(tokens) == 0 || end > tokens[len(tokens)-1].End) {
			tokens = append(tokens, editor.Token{End: end, Kind: kind})
		}
	}
	i := 0
	for i < len(line) {
		if state != 0 {
			kind := editor.TokenString
			n := int(state - 1)
			if state >= 128 {
				kind = editor.TokenComment
				n = int(state - 128)
			}
			close := "]" + strings.Repeat("=", n) + "]"
			if j := strings.Index(line[i:], close); j >= 0 {
				i += j + len(close)
				emit(i, kind)
				state = 0
				continue
			}
			emit(len(line), kind)
			return tokens, state
		}
		start := i
		comment := line[i] == '#'
		if comment {
			i++
		}
		if i < len(line) && line[i] == '[' {
			j := i + 1
			for j < len(line) && line[j] == '=' {
				j++
			}
			if j < len(line) && line[j] == '[' && j-i-1 < 126 {
				n := j - i - 1
				state = uint64(n + 1)
				if comment {
					state = uint64(n + 128)
				}
				i = j + 1
				continue
			}
		}
		if comment {
			emit(len(line), editor.TokenComment)
			return tokens, 0
		}
		if line[i] == '"' {
			i++
			for i < len(line) {
				if line[i] == '\\' && i+1 < len(line) {
					i += 2
					continue
				}
				if line[i] == '"' {
					i++
					break
				}
				i++
			}
			emit(i, editor.TokenString)
			continue
		}
		if strings.HasPrefix(line[i:], "${") || strings.HasPrefix(line[i:], "$ENV{") || strings.HasPrefix(line[i:], "$CACHE{") {
			if j := strings.IndexByte(line[i:], '}'); j >= 0 {
				i += j + 1
				emit(i, editor.TokenVariable)
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if unicode.IsLetter(r) || r == '_' {
			i += size
			for i < len(line) {
				r, n := utf8.DecodeRuneInString(line[i:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
					break
				}
				i += n
			}
			kind := editor.TokenText
			j := i
			for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
				j++
			}
			if j < len(line) && line[j] == '(' {
				kind = editor.TokenFunction
			}
			emit(i, kind)
			continue
		}
		i = start + size
		emit(i, editor.TokenText)
	}
	return tokens, state
}
