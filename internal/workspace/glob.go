package workspace

import (
	"path"
	"runtime"
	"strings"
)

// MatchVSCodeGlob reports whether rel, a slash-separated workspace-relative
// path, matches a VS Code glob. `**` is its own path segment and matches
// zero or more segments; braces expand alternates.
func MatchVSCodeGlob(pattern, rel string) bool {
	pattern = strings.TrimPrefix(strings.TrimSuffix(filepathToSlash(pattern), "/"), "/")
	rel = strings.TrimPrefix(filepathToSlash(rel), "/")
	if pattern == "" || rel == "" {
		return false
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		pattern, rel = strings.ToLower(pattern), strings.ToLower(rel)
	}
	for _, expanded := range expandGlobBraces(pattern) {
		if matchGlobSegments(strings.Split(expanded, "/"), strings.Split(rel, "/")) {
			return true
		}
	}
	return false
}

func filepathToSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

func matchGlobSegments(pattern, parts []string) bool {
	type state struct{ p, s int }
	memo := map[state]bool{}
	seen := map[state]bool{}
	var match func(p, s int) bool
	match = func(p, s int) bool {
		st := state{p, s}
		if seen[st] {
			return memo[st]
		}
		seen[st] = true
		var out bool
		switch {
		case p == len(pattern):
			out = s == len(parts)
		case pattern[p] == "**":
			for i := s; i <= len(parts) && !out; i++ {
				out = match(p+1, i)
			}
		case s < len(parts):
			if ok, err := path.Match(pattern[p], parts[s]); err == nil && ok {
				out = match(p+1, s+1)
			}
		}
		memo[st] = out
		return out
	}
	return match(0, 0)
}

func expandGlobBraces(pattern string) []string {
	start, end, depth := -1, -1, 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			i++
			continue
		}
		switch pattern[i] {
		case '{':
			if depth == 0 && start < 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				end = i
				i = len(pattern)
			}
		}
	}
	if start < 0 || end < 0 {
		return []string{pattern}
	}
	inner := pattern[start+1 : end]
	var alts []string
	depth, last := 0, 0
	for i := 0; i <= len(inner); i++ {
		if i == len(inner) || (inner[i] == ',' && depth == 0) {
			alts = append(alts, inner[last:i])
			last = i + 1
			continue
		}
		if inner[i] == '\\' {
			i++
			continue
		}
		if inner[i] == '{' {
			depth++
		} else if inner[i] == '}' {
			depth--
		}
	}
	if len(alts) < 2 {
		return []string{pattern}
	}
	var out []string
	for _, alt := range alts {
		for _, expanded := range expandGlobBraces(pattern[:start] + alt + pattern[end+1:]) {
			out = append(out, expanded)
		}
	}
	return out
}

// Excluded reports whether rel or one of its parent directories matches an
// enabled exclusion. Enabled exceptions, written with a leading `!` as in VS
// Code, win over exclusions regardless of their order.
func Excluded(patterns map[string]bool, rel string) bool {
	return ExcludedWithSiblings(patterns, nil, rel, nil)
}

// ExcludedWithSiblings applies boolean exclusions and VS Code sibling clauses.
// hasSibling answers whether a name exists next to the candidate file.
func ExcludedWithSiblings(patterns map[string]bool, clauses map[string]string, rel string, hasSibling func(string) bool) bool {
	return exclusionMatch(patterns, clauses, rel, hasSibling, false) && !exclusionMatch(patterns, clauses, rel, hasSibling, true)
}

// Excepted reports whether rel is inside an enabled `!` exception.
func Excepted(patterns map[string]bool, rel string) bool {
	return exclusionMatch(patterns, nil, rel, nil, true)
}

// ExceptedWithSiblings is Excepted for boolean and conditional exceptions.
func ExceptedWithSiblings(patterns map[string]bool, clauses map[string]string, rel string, hasSibling func(string) bool) bool {
	return exclusionMatch(patterns, clauses, rel, hasSibling, true)
}

func exclusionMatch(patterns map[string]bool, clauses map[string]string, rel string, hasSibling func(string) bool, exceptions bool) bool {
	rel = strings.TrimPrefix(filepathToSlash(rel), "/")
	for p := rel; p != "" && p != "."; {
		for pattern, enabled := range patterns {
			if !enabled || strings.HasPrefix(pattern, "!") != exceptions {
				continue
			}
			pattern = strings.TrimPrefix(pattern, "!")
			if MatchVSCodeGlob(pattern, p) {
				return true
			}
		}
		for pattern, when := range clauses {
			if strings.HasPrefix(pattern, "!") != exceptions || !MatchVSCodeGlob(strings.TrimPrefix(pattern, "!"), p) {
				continue
			}
			if hasSibling != nil && siblingClauseMatches(when, p, hasSibling) {
				return true
			}
		}
		i := strings.LastIndexByte(p, '/')
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return false
}

func siblingClauseMatches(when, rel string, hasSibling func(string) bool) bool {
	base := path.Base(rel)
	name := strings.TrimSuffix(base, path.Ext(base))
	sibling := strings.Replace(when, "$(basename)", name, 1)
	return hasSibling(path.Join(path.Dir(rel), filepathToSlash(sibling)))
}

// ExceptionMayReveal reports whether an enabled exception may name rel or a
// descendant of rel. Glob exceptions are deliberately conservative: their
// literal prefix decides, and a pattern with no literal prefix may descend
// anywhere.
func ExceptionMayReveal(patterns map[string]bool, rel string) bool {
	rel = strings.TrimPrefix(filepathToSlash(rel), "/")
	for pattern, enabled := range patterns {
		if !enabled || !strings.HasPrefix(pattern, "!") {
			continue
		}
		target := strings.TrimPrefix(strings.TrimPrefix(filepathToSlash(strings.TrimPrefix(pattern, "!")), "/"), "/")
		literalEnd := strings.IndexAny(target, "*?{}[]")
		literal := target
		if literalEnd >= 0 {
			literal = target[:literalEnd]
		}
		literal = strings.TrimSuffix(literal, "/")
		switch {
		case target == rel || strings.HasPrefix(target, rel+"/"):
			return true
		case strings.HasPrefix(literal, rel+"/") || strings.HasPrefix(rel, literal+"/") || literal == rel:
			return true
		case literal == "":
			segments := strings.Split(target, "/")
			first := ""
			for _, segment := range segments {
				if segment != "**" && !strings.ContainsAny(segment, "*?{}[]") {
					first = segment
					break
				}
			}
			if first == "" {
				return true
			}
			base := rel
			if i := strings.LastIndexByte(rel, '/'); i >= 0 {
				base = rel[i+1:]
			}
			if base == first {
				return true
			}
		}
	}
	return false
}
