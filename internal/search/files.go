package search

import (
	"cmp"
	"slices"
	"strings"

	"github.com/sahilm/fuzzy"
)

// Files matches every whitespace-separated term against a relative path.
// Terms containing / use path order; other terms prefer the filename.
func Files(paths []string, query string, limit int) []Scored {
	if limit <= 0 {
		return nil
	}
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return Fuzzy(paths, "", limit)
	}
	if len(terms) == 1 && !strings.Contains(terms[0], "/") {
		return Fuzzy(paths, terms[0], limit)
	}
	type candidate struct {
		result Scored
		score  int
	}
	candidates := make([]candidate, len(paths))
	for i, path := range paths {
		candidates[i].result.Path = path
	}
	for _, term := range terms {
		remaining := make([]string, len(candidates))
		for i, candidate := range candidates {
			remaining[i] = candidate.result.Path
		}
		pathOrder := strings.Contains(term, "/")
		var matches fuzzy.Matches
		if pathOrder {
			matches = fuzzy.FindNoSort(term, remaining)
		} else {
			matches = fuzzy.FindFromNoSort(term, nameFirst(remaining))
		}
		next := make([]candidate, 0, len(matches))
		for _, match := range matches {
			candidate := candidates[match.Index]
			hits := match.MatchedIndexes
			if !pathOrder {
				hits = unflip(candidate.result.Path, hits)
			}
			candidate.result.Hits = append(candidate.result.Hits, hits...)
			candidate.score += match.Score
			next = append(next, candidate)
		}
		candidates = next
		if len(candidates) == 0 {
			return nil
		}
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int { return cmp.Compare(b.score, a.score) })
	out := make([]Scored, 0, min(limit, len(candidates)))
	for _, candidate := range candidates[:min(limit, len(candidates))] {
		slices.Sort(candidate.result.Hits)
		candidate.result.Hits = slices.Compact(candidate.result.Hits)
		out = append(out, candidate.result)
	}
	return out
}
