package colorliteral

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseHex(t *testing.T) {
	got := Parse("#f09 #f09a #112233 #11223380 #0000")
	want := []Literal{
		{Start: 0, End: 4, Raw: "#f09", Color: Color{255, 0, 153, 255}},
		{Start: 5, End: 10, Raw: "#f09a", Color: Color{255, 0, 153, 170}},
		{Start: 11, End: 18, Raw: "#112233", Color: Color{17, 34, 51, 255}},
		{Start: 19, End: 28, Raw: "#11223380", Color: Color{17, 34, 51, 128}},
		{Start: 29, End: 34, Raw: "#0000", Color: Color{0, 0, 0, 0}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() = %#v, want %#v", got, want)
	}
}

func TestParseLimit(t *testing.T) {
	got := ParseLimit("#fff #000 #abc", 2)
	if len(got) != 2 || got[0].Raw != "#fff" || got[1].Raw != "#000" {
		t.Fatalf("ParseLimit() = %#v", got)
	}
}

func TestParseEmpty(t *testing.T) {
	if got := Parse(""); len(got) != 0 {
		t.Fatalf("Parse(empty) = %#v, want no literals", got)
	}
}

func TestParseFunctions(t *testing.T) {
	src := "rgb(255, 0, 128) rgba(100%, 0%, 50%, 25%) rgb(255 0 128 / 50%) rgba(100% 0% 50% / .25) hsl(0, 100%, 50%) hsla(0.5turn 100% 50% / 25%)"
	got := Parse(src)
	wantColors := []Color{
		{255, 0, 128, 255},
		{255, 0, 128, 64},
		{255, 0, 128, 128},
		{255, 0, 128, 64},
		{255, 0, 0, 255},
		{0, 255, 255, 64},
	}
	if len(got) != len(wantColors) {
		t.Fatalf("Parse() found %d literals, want %d: %#v", len(got), len(wantColors), got)
	}
	for i, want := range wantColors {
		if got[i].Color != want {
			t.Errorf("Parse()[%d].Color = %#v, want %#v (%q)", i, got[i].Color, want, got[i].Raw)
		}
	}
}

func TestParseBoundaries(t *testing.T) {
	src := "foo#fff #12345 #123456789 #abcdg #fff-tail #fffé #fff! myrgb(1,2,3) rgbx(1,2,3) rgb (1,2,3) rgb(1,2,3)px #abc"
	got := Parse(src)
	if len(got) != 2 || got[0].Raw != "#fff" || got[1].Raw != "#abc" {
		t.Fatalf("Parse(%q) = %#v", src, got)
	}
}

func TestParseRejectsInvalidFunctions(t *testing.T) {
	src := "rgb(NaN,0,0) rgba(1,2,3,Inf) rgb(256,0,0) rgb(1%,2,3) rgb(1 2 3 / 2) hsl(0,101%,50%) hsl(0 50 50%) rgba(1,2,3,)"
	if got := Parse(src); len(got) != 0 {
		t.Fatalf("Parse() accepted invalid functions: %#v", got)
	}
}

func TestParseRepeatedUnterminatedFunctionPrefixes(t *testing.T) {
	src := strings.Repeat("rgb(", 2048) + "rgb(1,2,3)"
	got := Parse(src)
	if len(got) != 1 || got[0].Raw != "rgb(1,2,3)" {
		t.Fatalf("Parse() = %#v, want the final valid rgb() literal", got)
	}
}

func TestParseMultipleWithUnicodeByteOffsets(t *testing.T) {
	src := "λ = \"#abc\"; color = rgb(0 128 255 / 50%)"
	got := Parse(src)
	if len(got) != 2 {
		t.Fatalf("Parse() found %d literals: %#v", len(got), got)
	}
	for _, literal := range got {
		if src[literal.Start:literal.End] != literal.Raw {
			t.Errorf("range [%d:%d] = %q, want %q", literal.Start, literal.End, src[literal.Start:literal.End], literal.Raw)
		}
	}
	if got[0].Start != 6 || got[0].End != 10 || got[1].Start != 21 {
		t.Fatalf("unexpected byte offsets: %#v", got)
	}
}
