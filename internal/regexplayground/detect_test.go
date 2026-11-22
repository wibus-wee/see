package regexplayground

import (
	"strings"
	"testing"
)

func TestDetectGoRegexpCompileLiteral(t *testing.T) {
	source := "package p\nimport \"regexp\"\nvar r = regexp.MustCompile(`(?P<word>\\w+)`)\n"
	offset := strings.Index(source, "word")
	got, ok := Detect(source, "sample.go", offset)
	if !ok || got.Pattern != `(?P<word>\w+)` || got.Flags != "" {
		t.Fatalf("Detect() = %+v, %v", got, ok)
	}
}

func TestDetectGoIgnoresUnrelatedStringsAndComments(t *testing.T) {
	source := "package p\nvar s = \"regexp.Compile(`foo`)\"\n// regexp.MustCompile(\"bar\")\n"
	if _, ok := Detect(source, "sample.go", strings.Index(source, "foo")); ok {
		t.Fatal("recognized a quoted Go string")
	}
}

func TestDetectJSRegexEscapesClassesAndFlags(t *testing.T) {
	source := `const ratio = a / b; const re = /https?:\/\/[^/]+/im;`
	offset := strings.Index(source, "https") + 2
	got, ok := Detect(source, "sample.ts", offset)
	if !ok || got.Pattern != `https?:\/\/[^/]+` || got.Flags != "im" || len(got.UnsupportedFlags) != 0 {
		t.Fatalf("Detect() = %+v, %v", got, ok)
	}
	class := `const slash = /[/]/g;`
	got, ok = Detect(class, "sample.js", strings.Index(class, "[/]")+1)
	if !ok || got.Pattern != `[/]` || got.Flags != "g" {
		t.Fatalf("character class slash = %+v, %v", got, ok)
	}
}

func TestDetectJSRejectsDivisionAndComments(t *testing.T) {
	source := "const ratio = left / right; // /not-a-regex/g\n"
	for _, fragment := range []string{"left / right", "/not-a-regex/g"} {
		if _, ok := Detect(source, "sample.js", strings.Index(source, fragment)+1); ok {
			t.Errorf("recognized %q as a regex", fragment)
		}
	}
}

func TestDetectJSReportsUnsupportedFlags(t *testing.T) {
	source := "const re = /x/uy;"
	got, ok := Detect(source, "sample.js", strings.Index(source, "x"))
	if !ok || strings.Join(got.UnsupportedFlags, "") != "uy" {
		t.Fatalf("Detect() = %+v, %v", got, ok)
	}
}

func TestUnsupportedJSFeaturesDoNotFallThroughToRE2(t *testing.T) {
	for _, pattern := range []string{`(?=word)`, `(?<word>\w+)`, `\1`, `\Aword`, `\p{L}`} {
		if got := UnsupportedJSFeatures(pattern); len(got) == 0 {
			t.Errorf("UnsupportedJSFeatures(%q) = empty", pattern)
		}
	}
	if got := UnsupportedJSFeatures(`(?P<word>\w+)`); len(got) == 0 {
		t.Fatal("Go-only named group syntax was not rejected")
	}
	if got := UnsupportedJSFeatures(`[?=]`); len(got) != 0 {
		t.Fatalf("character class was mistaken for unsupported syntax: %v", got)
	}
}

func TestDetectUnsupportedExtension(t *testing.T) {
	if _, ok := Detect("/x/", "sample.py", 1); ok {
		t.Fatal("recognized a regex in an unsupported format")
	}
}
