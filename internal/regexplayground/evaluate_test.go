package regexplayground

import (
	"strings"
	"testing"
)

func TestEvaluateNamedUnmatchedEmptyAndZeroWidthCaptures(t *testing.T) {
	got := Evaluate(`(?P<optional>x)?(?P<letters>a*)`, "a", Flags{})
	if got.Error != "" || len(got.Matches) != 1 {
		t.Fatalf("Evaluate() = %+v", got)
	}
	first := got.Matches[0]
	if len(first.Captures) != 3 || first.Captures[1].Name != "optional" || first.Captures[1].Matched || !first.Captures[2].Matched || first.Captures[2].Value != "a" {
		t.Fatalf("first match captures = %+v", first.Captures)
	}
	zeroWidth := Evaluate(`(?P<empty>)`, "", Flags{})
	if zeroWidth.Error != "" || len(zeroWidth.Matches) != 1 {
		t.Fatalf("zero-width matches = %+v", zeroWidth)
	}
	match := zeroWidth.Matches[0]
	if match.Start != 0 || match.End != 0 || !match.Captures[1].Matched || match.Captures[1].Start != match.Captures[1].End {
		t.Fatalf("zero-width capture = %+v", match)
	}
}

func TestEvaluateUnicodeByteRanges(t *testing.T) {
	sample := "café 世界"
	got := Evaluate(`\p{L}+`, sample, Flags{})
	if len(got.Matches) != 2 || got.Matches[0].Start != 0 || got.Matches[0].End != len("café") || got.Matches[0].Captures[0].Value != "café" || got.Matches[1].Captures[0].Value != "世界" {
		t.Fatalf("Unicode ranges = %+v", got)
	}
}

func TestEvaluateFlagsAndPatternError(t *testing.T) {
	if got := Evaluate("abc", "ABC", Flags{IgnoreCase: true}); len(got.Matches) != 1 {
		t.Fatalf("case-insensitive matches = %+v", got)
	}
	if got := Evaluate("(", "", Flags{}); got.Error == "" {
		t.Fatalf("invalid pattern was not reported: %+v", got)
	}
}

func TestEvaluateEmptyPatternAndSample(t *testing.T) {
	got := Evaluate("", "", Flags{})
	if got.Error != "" || len(got.Matches) != 1 || got.Matches[0].Start != 0 || got.Matches[0].End != 0 {
		t.Fatalf("empty input = %+v", got)
	}
}

func TestEvaluateLimitsAndMatchCap(t *testing.T) {
	if got := Evaluate(strings.Repeat("a", MaxPatternBytes+1), "", Flags{}); got.Limited == "" || len(got.Matches) != 0 {
		t.Fatalf("pattern limit = %+v", got)
	}
	if got := Evaluate("a", strings.Repeat("a", MaxSampleBytes+1), Flags{}); got.Limited == "" || len(got.Matches) != 0 {
		t.Fatalf("sample limit = %+v", got)
	}
	got := Evaluate("a", strings.Repeat("a", MaxMatches+7), Flags{})
	if !got.Truncated || len(got.Matches) != MaxMatches {
		t.Fatalf("match limit = %d matches, truncated %v", len(got.Matches), got.Truncated)
	}
}
