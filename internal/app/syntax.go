package app

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
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

const languageSampleSize = 8192

func languageFor(name, text string) *editor.Language {
	if len(text) > languageSampleSize {
		text = text[:languageSampleSize]
	}
	if filepath.Base(name) == "CMakeLists.txt" || strings.EqualFold(filepath.Ext(name), ".cmake") {
		return cmakeLanguage
	}
	if lang := editor.LanguageForText(name, text); lang != nil {
		return lang
	}
	return languageFromContent(text)
}

var (
	cmakeSourcePattern      = regexp.MustCompile(`(?im)^\s*(cmake_minimum_required|project|add_executable|add_library|find_package|target_(include_directories|link_libraries|compile_definitions|sources)|add_subdirectory)\s*\(`)
	goSourcePattern         = regexp.MustCompile(`(?m)^\s*package\s+[A-Za-z_][A-Za-z0-9_]*\b`)
	typeScriptSourcePattern = regexp.MustCompile(`(?m)^\s*(interface\s+[A-Za-z_]\w*|type\s+[A-Za-z_]\w*\s*=|enum\s+[A-Za-z_]\w*)`)
	javaScriptSourcePattern = regexp.MustCompile(`(?m)^\s*(const\s|let\s|var\s|function\s|export\s+(default|const|function|class)|import\s+.*\s+from\s+)`)
	pythonSourcePattern     = regexp.MustCompile(`(?m)^\s*(def\s+[A-Za-z_]\w*\s*\(|class\s+[A-Za-z_]\w*\s*[:(]|from\s+\S+\s+import\s+|import\s+\S+|if\s+__name__\s*==)`)
	rustSourcePattern       = regexp.MustCompile(`(?m)^\s*(fn\s+main\s*\(|pub\s+fn\s+|use\s+std::|let\s+mut\s+)`)
	cppSourcePattern        = regexp.MustCompile(`(?m)^\s*(#include\s*<iostream>|using\s+namespace\s+std;)`)
	cSourcePattern          = regexp.MustCompile(`(?m)^\s*#include\s*<(stdio|stdlib|string)\.h>`)
	shellSourcePattern      = regexp.MustCompile(`(?m)^\s*(echo\s|export\s+[A-Za-z_]|set\s+-[a-zA-Z]*e|#!.*\b(sh|bash|zsh)\b)`)
	rubySourcePattern       = regexp.MustCompile(`(?m)^\s*(def\s+[A-Za-z_]\w*\s*$|require\s+['"])`)
	sqlSourcePattern        = regexp.MustCompile(`(?i)(^|\n)\s*(select\s+.+\s+from\s+|create\s+(table|database)\s+|insert\s+into\s+|update\s+\S+\s+set\s+|delete\s+from\s+)`)
	markdownSourcePattern   = regexp.MustCompile("(?m)^\\s*(#{1,6}\\s|```|[-*+]\\s|>\\s)")
	yamlMappingPattern      = regexp.MustCompile(`(?m)^\s*[A-Za-z_][A-Za-z0-9_.-]*:\s+\S+`)
	tomlSourcePattern       = regexp.MustCompile(`(?m)^\s*\[[A-Za-z0-9_.-]+\]\s*$`)
	cssSourcePattern        = regexp.MustCompile(`(?s)[.#]?[A-Za-z][A-Za-z0-9_-]*\s*\{[^}]*[A-Za-z-]+\s*:`)
	htmlSourcePattern       = regexp.MustCompile(`(?i)^<(html|head|body|div|p|main|section|article|script|style)(\s|>)`)
)

func languageFromContent(text string) *editor.Language {
	if len(text) > languageSampleSize {
		text = text[:languageSampleSize]
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	name := ""
	switch {
	case (strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")) && json.Valid([]byte(text)):
		name = "JSON"
	case strings.HasPrefix(strings.ToLower(text), "<?xml"):
		name = "XML"
	case strings.HasPrefix(strings.ToLower(text), "<!doctype html") || htmlSourcePattern.MatchString(text):
		name = "HTML"
	case cmakeSourcePattern.MatchString(text):
		name = "CMake"
	case goSourcePattern.MatchString(text):
		name = "Go"
	case rustSourcePattern.MatchString(text):
		name = "Rust"
	case typeScriptSourcePattern.MatchString(text):
		name = "TypeScript"
	case javaScriptSourcePattern.MatchString(text) || strings.Contains(text, "console.log("):
		name = "JavaScript"
	case pythonSourcePattern.MatchString(text) || strings.HasPrefix(text, "print("):
		name = "Python"
	case cppSourcePattern.MatchString(text):
		name = "C++"
	case cSourcePattern.MatchString(text):
		name = "C"
	case shellSourcePattern.MatchString(text):
		name = "Shell"
	case rubySourcePattern.MatchString(text):
		name = "Ruby"
	case sqlSourcePattern.MatchString(text):
		name = "SQL"
	case strings.HasPrefix(text, "---\n") || len(yamlMappingPattern.FindAllString(text, 2)) == 2:
		name = "YAML"
	case tomlSourcePattern.MatchString(text):
		name = "TOML"
	case markdownSourcePattern.MatchString(text):
		name = "Markdown"
	case cssSourcePattern.MatchString(text):
		name = "CSS"
	}
	if name == "" {
		return nil
	}
	if name == "CMake" {
		return cmakeLanguage
	}
	return editor.LanguageNamed(name)
}

func documentLanguages() []*editor.Language {
	languages := slices.Clone(editor.Languages())
	if !slices.Contains(languages, cmakeLanguage) {
		languages = append(languages, cmakeLanguage)
	}
	slices.SortFunc(languages, func(a, b *editor.Language) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return languages
}

func documentLanguage(d *doc) *editor.Language {
	if d == nil || d.buf == nil {
		return nil
	}
	lang := d.buf.Language()
	if lang == d.wrappedLang && d.baseLang != nil {
		return d.baseLang
	}
	return lang
}

func (a *App) setDocumentLanguage(d *doc) {
	if d == nil || d.buf == nil {
		return
	}
	oldLanguage := documentLanguage(d)
	var lang *editor.Language
	switch {
	case d.plain:
	case d.languageManual:
		lang = d.manualLanguage
	default:
		lang = languageFor(d.name, languageSample(d.buf))
	}
	if d.wrappedLang != nil && d.buf.Language() == d.wrappedLang {
		if oldLanguage != lang {
			a.clearFoldingRanges(d)
			d.buf.SetLanguage(lang)
		}
	} else {
		d.buf.SetLanguage(lang)
	}
	d.detectedVersion, d.detectedName, d.languageChecked = d.buf.Version(), d.name, true
	if oldLanguage != lang {
		a.applyEditorPrefsAll()
	}
}

func languageSample(buf *editor.Buffer) string {
	snap := buf.Snapshot()
	n := min(snap.Len(), languageSampleSize)
	var sample strings.Builder
	sample.Grow(n)
	snap.Chunks(0, n, func(chunk string) bool {
		sample.WriteString(chunk)
		return true
	})
	return sample.String()
}

func (a *App) selectDocumentLanguage(d *doc, lang *editor.Language) {
	if d == nil || d.buf == nil {
		return
	}
	d.languageManual, d.manualLanguage = true, lang
	a.setDocumentLanguage(d)
}

func (a *App) detectDocumentLanguage(d *doc) {
	if d == nil || d.buf == nil || d.plain || d.languageManual || d.buf.Language() != nil {
		return
	}
	version := d.buf.Version()
	if d.languageChecked && d.detectedVersion == version && d.detectedName == d.name {
		return
	}
	a.setDocumentLanguage(d)
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
