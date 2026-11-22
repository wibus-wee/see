package app

import (
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type readerLine struct {
	start, end int
	text       string
	hidden     bool
}

type readerBlock struct {
	first, last int
	lines       []readerLine
}

type readerCache struct {
	version       uint64
	language      string
	offsets       []int
	blocks        []readerBlock
	font          ui.Font
	muted, accent ui.Color
	layouts       map[int]readerLayout
}

type readerLayout struct {
	spans []ui.Span
	width float32
}

func readerDirective(s string) bool {
	s = strings.ToLower(strings.TrimSpace(strings.TrimLeft(s, "/!")))
	for _, prefix := range []string{"go:", "nolint", "lint:", "+build", "export ", "#region", "#endregion", "region", "endregion"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func readerBlocks(text, language string) []readerBlock {
	language = strings.ToLower(language)
	if language == "python" {
		return pythonReaderBlocks(text)
	}
	switch language {
	case "go", "c", "c++", "objective-c", "objective-c++", "javascript", "typescript", "javascriptreact", "typescriptreact", "java", "kotlin", "swift", "c#", "rust", "scala", "dart", "php":
	default:
		return nil
	}
	lines := strings.Split(text, "\n")
	offsets := readerOffsets(lines)
	// The lexer keeps comment-shaped text inside strings out of reader mode.
	comments := make([]bool, len(lines))
	lang := editor.LanguageNamed(language)
	if lang == nil || lang.Tokenizer == nil {
		return nil
	}
	var state uint64
	for i, line := range lines {
		var tokens []editor.Token
		tokens, state = lang.Tokenizer.Tokenize(line, state, nil)
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		start := 0
		for _, token := range tokens {
			if start <= indent && indent < token.End {
				comments[i] = token.Kind == editor.TokenComment
				break
			}
			start = token.End
		}
	}
	var blocks []readerBlock
	for i := 0; i < len(lines); {
		s := strings.TrimSpace(lines[i])
		if !comments[i] {
			i++
			continue
		}
		if strings.HasPrefix(s, "//") {
			first, always := i, false
			var rendered []readerLine
			for i < len(lines) && comments[i] && strings.HasPrefix(strings.TrimSpace(lines[i]), "//") {
				raw := strings.TrimLeft(lines[i], " \t")
				if !readerDirective(raw) {
					marker := 2
					if strings.HasPrefix(raw, "///") || strings.HasPrefix(raw, "//!") {
						marker = 3
						always = true
					}
					body := strings.TrimPrefix(raw[marker:], " ")
					if strings.HasPrefix(raw[marker:], "    ") {
						body = raw[marker:]
					}
					rendered = append(rendered, readerSourceLine(lines, offsets, i, body))
				}
				i++
			}
			if len(rendered) > 0 && (always || i < len(lines) && strings.TrimSpace(lines[i]) != "" && !comments[i]) {
				blocks = append(blocks, readerBlock{first, i - 1, rendered})
			}
			continue
		}
		if strings.HasPrefix(s, "/**") && !strings.HasPrefix(s, "/**/") {
			first := i
			indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
			var rendered []readerLine
			closed := false
			for ; i < len(lines); i++ {
				body := strings.TrimSpace(lines[i])
				if i == first {
					body = strings.TrimPrefix(body, "/**")
				} else {
					body = strings.TrimPrefix(body, "*")
				}
				if end := strings.Index(body, "*/"); end >= 0 {
					if strings.TrimSpace(body[end+2:]) != "" {
						break
					}
					body = body[:end]
					closed = true
				} else if strings.TrimSpace(lines[i]) == "*/" {
					body = ""
					closed = true
				}
				line := readerSourceLine(lines, offsets, i, strings.TrimSpace(body))
				line.start = min(line.start, offsets[i]+indent)
				if line.text == "" && (i == first || closed) {
					line = readerHiddenLine(lines, offsets, i)
				}
				rendered = append(rendered, line)
				if closed {
					i++
					break
				}
			}
			if closed {
				blocks = append(blocks, readerBlock{first, i - 1, rendered})
			}
			if i == first {
				i++
			}
			continue
		}
		i++
	}
	return blocks
}

func readerOffsets(lines []string) []int {
	offsets := make([]int, len(lines))
	for i := 1; i < len(lines); i++ {
		offsets[i] = offsets[i-1] + len(lines[i-1]) + 1
	}
	return offsets
}

func readerSourceLine(lines []string, offsets []int, i int, body string) readerLine {
	indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
	return readerLine{start: offsets[i] + indent, end: offsets[i] + len(strings.TrimSuffix(lines[i], "\r")), text: body}
}

func readerHiddenLine(lines []string, offsets []int, i int) readerLine {
	end := offsets[i] + len(lines[i])
	if i+1 < len(offsets) {
		end = offsets[i+1]
	}
	return readerLine{start: offsets[i], end: end, hidden: true}
}

func pythonReaderBlocks(text string) []readerBlock {
	lines := strings.Split(text, "\n")
	offsets := readerOffsets(lines)
	continuation := make([]bool, len(lines))
	var state uint64
	for i, line := range lines {
		continuation[i] = state != 0
		_, state = editor.LanguageNamed("Python").Tokenizer.Tokenize(line, state, nil)
	}
	var blocks []readerBlock
	expect, parentIndent := true, -1
	for i := 0; i < len(lines); i++ {
		if continuation[i] {
			continue
		}
		s := strings.TrimSpace(lines[i])
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
		quote := ""
		if strings.HasPrefix(s, `"""`) {
			quote = `"""`
		} else if strings.HasPrefix(s, "'''") {
			quote = "'''"
		}
		if quote != "" {
			first, eligible := i, expect && indent > parentIndent
			var rendered []readerLine
			closed := false
			for ; i < len(lines); i++ {
				body := strings.TrimSpace(lines[i])
				if i == first {
					body = strings.TrimPrefix(body, quote)
				}
				if end := strings.Index(body, quote); end >= 0 {
					eligible = eligible && strings.TrimSpace(body[end+3:]) == ""
					body = body[:end]
					closed = true
				}
				line := readerSourceLine(lines, offsets, i, body)
				if strings.TrimSpace(body) == "" && (i == first || closed) {
					line = readerHiddenLine(lines, offsets, i)
				}
				rendered = append(rendered, line)
				if closed {
					break
				}
			}
			if eligible && closed {
				blocks = append(blocks, readerBlock{first, i, rendered})
			}
			expect = false
			continue
		}
		expect = (strings.HasPrefix(s, "def ") || strings.HasPrefix(s, "async def ") || strings.HasPrefix(s, "class ")) && strings.HasSuffix(s, ":")
		parentIndent = indent
	}
	return blocks
}

type readerSpan struct {
	text                     string
	bold, italic, code, link bool
}

var readerInline = regexp.MustCompile("`([^`]+)`|\\*\\*([^*]+)\\*\\*|\\*([^*]+)\\*|_([^_]+)_|\\[([^]\\n]+)\\](?:\\(([^)]+)\\))?|\\{@(code|link) ([^}]+)\\}")

func readerSpans(text string) []readerSpan {
	if strings.HasPrefix(text, "\t") || strings.HasPrefix(text, "    ") {
		return []readerSpan{{text: strings.ReplaceAll(text, "\t", "    "), code: true}}
	}
	if strings.HasPrefix(text, "# ") {
		return []readerSpan{{text: strings.TrimPrefix(text, "# "), bold: true}}
	}
	if strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "* ") {
		text = "• " + text[2:]
	}
	var out []readerSpan
	if strings.HasPrefix(text, "@") {
		fields := strings.Fields(text)
		if len(fields) > 0 {
			label, rest := "", strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
			switch fields[0] {
			case "@param":
				if strings.HasPrefix(rest, "{") {
					if end := strings.Index(rest, "}"); end >= 0 {
						rest = strings.TrimSpace(rest[end+1:])
					}
				}
				if parts := strings.Fields(rest); len(parts) > 0 {
					label = parts[0]
					rest = strings.TrimSpace(strings.TrimPrefix(rest, label))
					rest = strings.TrimPrefix(rest, "- ")
				}
			case "@return", "@returns":
				label = "Returns"
			case "@throws":
				label = "Throws"
			}
			if label != "" {
				out = append(out, readerSpan{text: label, bold: true})
				text = " " + rest
			}
		}
	}
	// Consume matches in source order, keeping malformed markup literal.
	pos := 0
	for _, m := range readerInline.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > pos {
			out = append(out, readerSpan{text: text[pos:m[0]]})
		}
		span := readerSpan{}
		for group := 1; group <= 5; group++ {
			if m[group*2] >= 0 {
				span.text = text[m[group*2]:m[group*2+1]]
				span.code = group == 1
				span.bold = group == 2
				span.italic = group == 3 || group == 4
				span.link = group == 5
				break
			}
		}
		if m[14] >= 0 {
			span.text = text[m[16]:m[17]]
			span.code = text[m[14]:m[15]] == "code"
			span.link = !span.code
		}
		out = append(out, span)
		pos = m[1]
	}
	if pos < len(text) {
		out = append(out, readerSpan{text: text[pos:]})
	}
	return out
}

func readerBlockRaw(block readerBlock, selections []editor.Selection, offsets []int) bool {
	start := offsets[block.first]
	end := offsets[block.last+1] - 1
	for _, sel := range selections {
		if min(sel.Anchor, sel.Head) <= end && max(sel.Anchor, sel.Head) >= start {
			return true
		}
	}
	return false
}

func mergeReaderReplacements(colors, reader []editor.Replacement) []editor.Replacement {
	out := append([]editor.Replacement(nil), colors...)
	for _, r := range reader {
		overlap := false
		for _, c := range colors {
			if r.Start < c.End && c.Start < r.End {
				overlap = true
				break
			}
		}
		if !overlap {
			out = append(out, r)
		}
	}
	return out
}

func (a *App) readerReplacements(c *ui.Context, d *doc, st *editor.State) []editor.Replacement {
	if a.cfg.HideDocComments || d.buf.Language() == nil {
		return nil
	}
	language := d.buf.Language().Name
	if d.readerCache == nil || d.readerCache.version != d.buf.Version() || d.readerCache.language != language {
		text := d.buf.String()
		offsets := readerOffsets(strings.Split(text, "\n"))
		offsets = append(offsets, len(text)+1)
		d.readerCache = &readerCache{version: d.buf.Version(), language: language, offsets: offsets, blocks: readerBlocks(text, language)}
	}
	blocks := d.readerCache.blocks
	cache := d.readerCache
	muted := c.Theme().Text.Alpha(0.72)
	if cache.layouts == nil || cache.font != st.Font || cache.muted != muted || cache.accent != c.Theme().Accent {
		cache.font, cache.muted, cache.accent = st.Font, muted, c.Theme().Accent
		cache.layouts = make(map[int]readerLayout)
	}
	sels := st.Selections()
	var replacements []editor.Replacement
	for _, block := range blocks {
		if readerBlockRaw(block, sels, d.readerCache.offsets) {
			continue
		}
		for _, line := range block.lines {
			if line.end <= line.start {
				continue
			}
			if line.hidden {
				replacements = append(replacements, editor.Replacement{Start: line.start, End: line.end})
				continue
			}
			layout, ok := cache.layouts[line.start]
			if !ok {
				var spans []ui.Span
				for _, part := range readerSpans(line.text) {
					span := ui.Span{Text: part.text, Size: st.Font.Size, Color: muted, Italic: part.italic}
					if span.Size <= 0 {
						span.Size = 13
					}
					if part.bold {
						span.Weight = 600
					}
					if part.link {
						span.Color = c.Theme().Accent
					}
					if part.code {
						span.Font = st.Font.Family
						if span.Font == "" {
							span.Font = "monospace"
						}
						span.Background = c.Theme().Text.Alpha(0.06)
						span.Radius = 2
					}
					spans = append(spans, span)
				}
				width, _ := ui.ShapeRichText(spans, ui.Font{Size: st.Font.Size}, 0).Size()
				layout = readerLayout{spans: spans, width: max(1, width)}
				cache.layouts[line.start] = layout
			}
			start := line.start
			replacements = append(replacements, editor.Replacement{Start: start, End: line.end, Width: layout.width, Interactive: true, Label: strings.TrimSpace(line.text), Content: func(c *ui.Context, e ui.Element, r editor.Replacement) {
				e.Cursor(ui.CursorText).OnClick(func() { st.SetSelections(editor.Selection{Anchor: r.Start, Head: r.Start}) }).Children(func() { ui.RichText(c, layout.spans...).FontSize(st.Font.Size) })
			}})
		}
	}
	return replacements
}
