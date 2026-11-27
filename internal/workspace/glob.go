package workspace

import (
	"path"
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
// enabled pattern: excluding a folder excludes everything under it.
func Excluded(patterns map[string]bool, rel string) bool {
	rel = strings.TrimPrefix(filepathToSlash(rel), "/")
	for p := rel; p != "" && p != "."; {
		for pattern, enabled := range patterns {
			if enabled && MatchVSCodeGlob(pattern, p) {
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
