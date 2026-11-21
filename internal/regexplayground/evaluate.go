package regexplayground

import (
	"fmt"
	"regexp"
)

const (
	MaxPatternBytes = 4096
	MaxSampleBytes  = 128 * 1024
	MaxMatches      = 100
)

// Flags are the RE2-compatible flags exposed by the playground.
type Flags struct {
	IgnoreCase, Multiline, DotAll bool
}

// Capture describes one numbered capture, including capture zero (the match).
type Capture struct {
	Number  int
	Name    string
	Start   int
	End     int
	Value   string
	Matched bool
}

// Match is a complete match and its numbered and named captures.
type Match struct {
	Start, End int
	Captures   []Capture
}

// Result contains bounded matches or a validation/limit message.
type Result struct {
	Matches   []Match
	Error     string
	Truncated bool
	Limited   string
}

// Evaluate runs a Go RE2 expression over sample, returning at most
// MaxMatches and never scanning inputs above the published byte limits.
func Evaluate(pattern, sample string, flags Flags) Result {
	if len(pattern) > MaxPatternBytes {
		return Result{Limited: fmt.Sprintf("pattern exceeds %d bytes", MaxPatternBytes)}
	}
	if len(sample) > MaxSampleBytes {
		return Result{Limited: fmt.Sprintf("sample exceeds %d bytes", MaxSampleBytes)}
	}
	if flags.IgnoreCase || flags.Multiline || flags.DotAll {
		prefix := "(?"
		if flags.IgnoreCase {
			prefix += "i"
		}
		if flags.Multiline {
			prefix += "m"
		}
		if flags.DotAll {
			prefix += "s"
		}
		pattern = prefix + ")" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return Result{Error: err.Error()}
	}
	indices := re.FindAllStringSubmatchIndex(sample, MaxMatches+1)
	truncated := len(indices) > MaxMatches
	if truncated {
		indices = indices[:MaxMatches]
	}
	names := re.SubexpNames()
	matches := make([]Match, 0, len(indices))
	for _, index := range indices {
		match := Match{Start: index[0], End: index[1], Captures: make([]Capture, 0, len(index)/2)}
		for i := 0; i+1 < len(index); i += 2 {
			capture := Capture{Number: i / 2, Start: index[i], End: index[i+1]}
			if capture.Start >= 0 && capture.End >= 0 {
				capture.Matched = true
				capture.Value = sample[capture.Start:capture.End]
			}
			if capture.Number < len(names) {
				capture.Name = names[capture.Number]
			}
			match.Captures = append(match.Captures, capture)
		}
		matches = append(matches, match)
	}
	return Result{Matches: matches, Truncated: truncated}
}
