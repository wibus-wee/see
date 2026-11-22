package search

import (
	"cmp"
	"runtime"
	"slices"
	"strings"
	"sync"

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
	workers := min(runtime.GOMAXPROCS(0), 8, max(1, len(paths)/4096))
	if workers == 1 {
		if len(terms) == 1 && !strings.Contains(terms[0], "/") {
			return Fuzzy(paths, terms[0], limit)
		}
		return mergeFileRanks([][]fileRank{rankFiles(paths, terms, limit)}, limit)
	}
	ranks := make([][]fileRank, workers)
	var wg sync.WaitGroup
	for i := range ranks {
		start, end := i*len(paths)/workers, (i+1)*len(paths)/workers
		wg.Go(func() { ranks[i] = rankFiles(paths[start:end], terms, limit) })
	}
	wg.Wait()
	out := mergeFileRanks(ranks, limit)
	if out == nil && len(terms) == 1 && !strings.Contains(terms[0], "/") {
		return []Scored{}
	}
	return out
}

type fileRank struct {
	result Scored
	score  int
	index  int
}

func rankFiles(paths, terms []string, limit int) []fileRank {
	term := terms[0]
	pathOrder := strings.Contains(term, "/")
	matches := matchFiles(paths, term, pathOrder)
	if len(terms) == 1 {
		slices.SortFunc(matches, func(a, b fuzzy.Match) int {
			if a.Score != b.Score {
				return cmp.Compare(b.Score, a.Score)
			}
			return cmp.Compare(a.Index, b.Index)
		})
		matches = matches[:min(limit, len(matches))]
		ranks := make([]fileRank, len(matches))
		for i, match := range matches {
			path, hits := paths[match.Index], match.MatchedIndexes
			if !pathOrder {
				hits = unflip(path, hits)
			}
			ranks[i] = fileRank{Scored{Path: path, Hits: hits}, match.Score, match.Index}
		}
		return ranks
	}
	ranks := make([]fileRank, len(matches))
	for i, match := range matches {
		path := paths[match.Index]
		ranks[i] = fileRank{
			Scored{Path: path, Hits: appendFileHits(nil, path, match.MatchedIndexes, pathOrder)},
			match.Score, match.Index,
		}
	}
	for _, term := range terms[1:] {
		if len(ranks) == 0 {
			return nil
		}
		remaining := make([]string, len(ranks))
		for i, rank := range ranks {
			remaining[i] = rank.result.Path
		}
		pathOrder := strings.Contains(term, "/")
		matches := matchFiles(remaining, term, pathOrder)
		// Matches arrive in input order, so compacting cannot overwrite a later match.
		next := ranks[:0]
		for _, match := range matches {
			candidate := ranks[match.Index]
			candidate.result.Hits = appendFileHits(candidate.result.Hits, candidate.result.Path, match.MatchedIndexes, pathOrder)
			candidate.score += match.Score
			next = append(next, candidate)
		}
		ranks = next
	}
	slices.SortFunc(ranks, func(a, b fileRank) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		return cmp.Compare(a.index, b.index)
	})
	ranks = ranks[:min(limit, len(ranks))]
	for i := range ranks {
		hits := ranks[i].result.Hits
		slices.Sort(hits)
		ranks[i].result.Hits = slices.Compact(hits)
	}
	return ranks
}

func matchFiles(paths []string, term string, pathOrder bool) fuzzy.Matches {
	if pathOrder {
		return fuzzy.FindNoSort(term, paths)
	}
	return fuzzy.FindFromNoSort(term, nameFirst(paths))
}

func appendFileHits(dst []int, path string, hits []int, pathOrder bool) []int {
	if pathOrder {
		return append(dst, hits...)
	}
	return appendUnflipped(dst, path, hits)
}

func mergeFileRanks(ranks [][]fileRank, limit int) []Scored {
	total := 0
	for _, rank := range ranks {
		total += len(rank)
	}
	if total == 0 {
		return nil
	}
	out := make([]Scored, 0, min(limit, total))
	for len(out) < cap(out) {
		best := -1
		for i, rank := range ranks {
			// Earlier shards win ties to preserve input order.
			if len(rank) > 0 && (best < 0 || rank[0].score > ranks[best][0].score) {
				best = i
			}
		}
		out = append(out, ranks[best][0].result)
		ranks[best] = ranks[best][1:]
	}
	return out
}
