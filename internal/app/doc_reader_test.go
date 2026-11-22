package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestReaderDetection(t *testing.T) {
	for _, language := range []string{"Go", "C", "C++", "JavaScript", "TypeScript", "Java", "Kotlin", "Swift", "C#", "Rust"} {
		t.Run(language, func(t *testing.T) {
			blocks := readerBlocks("// A `value`.\n//\n// More details.\nfunc example() {}", language)
			if len(blocks) != 1 || len(blocks[0].lines) != 3 || blocks[0].lines[0].text != "A `value`." {
				t.Fatalf("%+v", blocks)
			}
		})
	}
	for _, test := range []struct {
		name, language, text string
		count                int
	}{
		{"empty", "Go", "", 0},
		{"separated", "Go", "// ordinary\n\nfunc f() {}", 0},
		{"trailing", "Go", "x := 1 // ordinary", 0},
		{"directives", "Go", "//go:generate command\n//nolint\n//lint:ignore\n// +build linux\n//export Foo\n//#region\nfunc f() {}", 0},
		{"mixed directives", "Go", "// Useful.\n//nolint\nfunc f() {}", 1},
		{"rust doc", "Rust", "/// Docs\n\n", 1},
		{"rust inner", "Rust", "//! Docs", 1},
		{"csharp doc", "C#", "/// Docs\n", 1},
		{"jsdoc", "TypeScript", "/** Docs\n * @param name input\n */\nfunction f() {}", 1},
		{"single jsdoc", "JavaScript", "/** Docs */\nfunction f() {}", 1},
		{"ordinary block", "Java", "/* Not docs */", 0},
		{"empty block", "Java", "/**/", 0},
		{"unclosed", "Java", "/** broken", 0},
		{"code after block", "Java", "/** Docs */ int x;", 0},
		{"string", "Go", "var s = `\n// not a comment\ncode\n`", 0},
		{"markdown", "Markdown", "// docs\ncode", 0},
		{"text", "", "/** Docs */", 0},
		{"python module", "Python", "# header\n\"\"\"Module docs.\"\"\"\nx=1", 1},
		{"python function", "Python", "def f():\n    # comment\n    \"\"\"Summary.\n    Details.\n    \"\"\"\n    pass", 1},
		{"python class", "Python", "class C:\n    '''Class docs.'''\n    pass", 1},
		{"python async", "Python", "async def f():\n    '''Docs'''\n    pass", 1},
		{"python later string", "Python", "def f():\n    x=1\n    '''Not docs'''", 0},
		{"python wrong indent", "Python", "def f():\n'''Not docs'''", 0},
		{"python assignment", "Python", "x = '''Not docs'''", 0},
		{"python string with fake declaration", "Python", "x = '''\ndef fake():\n'''\n", 0},
		{"python unclosed", "Python", "\"\"\"broken", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := readerBlocks(test.text, test.language); len(got) != test.count {
				t.Fatalf("got %+v, want %d blocks", got, test.count)
			}
		})
	}
}

func TestReaderSourceRanges(t *testing.T) {
	text := "package p\n\n\t/**\n\t * Description.\n\t */\nfunc f() {}"
	blocks := readerBlocks(text, "Go")
	if len(blocks) != 1 {
		t.Fatalf("%+v", blocks)
	}
	for i, line := range blocks[0].lines {
		if line.end <= line.start || strings.HasPrefix(text[line.start:line.end], "\t") {
			t.Fatalf("bad range: %+v", line)
		}
		if i != 1 && line.text != "" {
			t.Fatalf("marker should be spacer: %+v", line)
		}
	}
}

func TestReaderBlockCommentAlignment(t *testing.T) {
	for _, indent := range []string{"", "\t", "    "} {
		t.Run(indent, func(t *testing.T) {
			text := indent + "/**\n" + indent + " * Description.\n" + indent + " *\n" + indent + " */\n" + indent + "function f() {}"
			blocks := readerBlocks(text, "TypeScript")
			if len(blocks) != 1 || len(blocks[0].lines) != 4 {
				t.Fatalf("unexpected blocks: %+v", blocks)
			}
			offsets := readerOffsets(strings.Split(text, "\n"))
			for i, line := range blocks[0].lines {
				if line.start != offsets[i]+len(indent) {
					t.Errorf("line %d starts at column %d, want %d", i, line.start-offsets[i], len(indent))
				}
			}
			if got := text[blocks[0].lines[1].start:blocks[0].lines[1].end]; got != " * Description." {
				t.Fatalf("replacement must cover the space before the star: %q", got)
			}
		})
	}
}

func TestReaderInlineFormatting(t *testing.T) {
	for _, test := range []struct {
		input string
		want  []readerSpan
	}{
		{"", nil},
		{"plain", []readerSpan{{text: "plain"}}},
		{"use `x`", []readerSpan{{text: "use "}, {text: "x", code: true}}},
		{"**bold** *italic* _also_", []readerSpan{{text: "bold", bold: true}, {text: " "}, {text: "italic", italic: true}, {text: " "}, {text: "also", italic: true}}},
		{"[site](https://example.com) [Name]", []readerSpan{{text: "site", link: true}, {text: " "}, {text: "Name", link: true}}},
		{"{@code x} {@link Foo}", []readerSpan{{text: "x", code: true}, {text: " "}, {text: "Foo", link: true}}},
		{"# Heading", []readerSpan{{text: "Heading", bold: true}}},
		{"- first", []readerSpan{{text: "• first"}}},
		{"* first", []readerSpan{{text: "• first"}}},
		{"1. first", []readerSpan{{text: "1. first"}}},
		{"    x := 1", []readerSpan{{text: "    x := 1", code: true}}},
		{"@param name description", []readerSpan{{text: "name", bold: true}, {text: " description"}}},
		{"@param {string} name - description", []readerSpan{{text: "name", bold: true}, {text: " description"}}},
		{"@returns result", []readerSpan{{text: "Returns", bold: true}, {text: " result"}}},
		{"@throws Error", []readerSpan{{text: "Throws", bold: true}, {text: " Error"}}},
		{"`broken ** [x", []readerSpan{{text: "`broken ** [x"}}},
	} {
		t.Run(test.input, func(t *testing.T) {
			if got := readerSpans(test.input); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestReaderRawSelections(t *testing.T) {
	text := "package p\n\n// Docs\n// more\nfunc f() {}"
	block := readerBlocks(text, "Go")[0]
	offsets := append(readerOffsets(strings.Split(text, "\n")), len(text)+1)
	for _, test := range []struct {
		anchor, head int
		raw          bool
	}{{0, 0, false}, {12, 12, true}, {0, 15, true}, {30, 15, true}, {len(text), len(text), false}} {
		if got := readerBlockRaw(block, []editor.Selection{{Anchor: test.anchor, Head: test.head}}, offsets); got != test.raw {
			t.Errorf("%+v: %v", test, got)
		}
	}
	if readerBlockRaw(block, nil, offsets) {
		t.Fatal("no selection should render")
	}
}

func TestReaderReplacementMerge(t *testing.T) {
	colors := []editor.Replacement{{Start: 5, End: 6}}
	reader := []editor.Replacement{{Start: 0, End: 10}, {Start: 10, End: 20}}
	if got := mergeReaderReplacements(colors, reader); len(got) != 2 || got[1].Start != 10 {
		t.Fatalf("%+v", got)
	}
	if got := mergeReaderReplacements(nil, nil); len(got) != 0 {
		t.Fatal(got)
	}
	if got := mergeReaderReplacements(nil, reader); len(got) != 2 {
		t.Fatal(got)
	}
}

func TestReaderModeCommand(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"main.go": "package main\n"}))
	a := h.a
	found := false
	for _, cmd := range commands() {
		if cmd.name == "Toggle Reader Mode" {
			found = true
			if cmd.shortcutKey != 0 {
				t.Fatal("unexpected shortcut")
			}
			cmd.run(a)
			if !a.cfg.HideDocComments {
				t.Fatal("command did not disable reader mode")
			}
		}
	}
	if !found {
		t.Fatal("missing command")
	}
}

func TestReaderModeEditorRevealAndToggle(t *testing.T) {
	text := "package example\n\n// Description with `code`.\n// More details.\nfunc f() {}\n"
	h := newHarness(t, writeTree(t, map[string]string{"main.go": text}))
	d := h.open("main.go")
	st := h.a.activeEditor()
	h.tt.Frame()
	if got := st.Replacements(); len(got) != 2 {
		t.Fatalf("rendered replacements: %+v", got)
	}
	st.SetSelections(editor.Selection{Anchor: strings.Index(text, "Description"), Head: strings.Index(text, "Description")})
	h.tt.Frame()
	if got := st.Replacements(); len(got) != 0 {
		t.Fatalf("caret did not reveal block: %+v", got)
	}
	st.SetSelections(editor.Selection{})
	h.tt.Frame()
	if got := st.Replacements(); len(got) != 2 {
		t.Fatalf("moving out did not render: %+v", got)
	}
	h.a.cfg.HideDocComments = true
	h.tt.Frame()
	if got := st.Replacements(); len(got) != 0 {
		t.Fatalf("toggle did not reveal source: %+v", got)
	}
	if d.buf.String() != text {
		t.Fatal("reader mode changed source")
	}
}
