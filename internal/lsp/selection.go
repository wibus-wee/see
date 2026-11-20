package lsp

// RangesOf flattens a selection range chain: the range it names first,
// then each parent's, innermost out.
func RangesOf(s *SelectionRange) []Range {
	var out []Range
	for ; s != nil; s = s.Parent {
		out = append(out, s.Range)
	}
	return out
}
