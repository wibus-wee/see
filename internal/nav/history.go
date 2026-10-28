// Package nav keeps the back and forward history of reading positions.
package nav

// Entry is a reading position: a document, a line and a byte column, both
// from 0.
type Entry struct {
	Path       string
	Line       int
	ByteColumn int
}

// DefaultLimit is how many entries each direction keeps.
const DefaultLimit = 100

// History is a browser-like back and forward stack with a bound.
type History struct {
	Back    []Entry
	Forward []Entry
	Limit   int
}

func (h *History) limit() int {
	if h.Limit <= 0 {
		return DefaultLimit
	}
	return h.Limit
}

// Push records from, the position left by a jump, and clears the forward
// history. A position equal to the last one recorded is not repeated.
func (h *History) Push(from Entry) {
	h.Forward = h.Forward[:0]
	if n := len(h.Back); n > 0 && h.Back[n-1] == from {
		return
	}
	h.Back = appendBounded(h.Back, from, h.limit())
}

// GoBack returns the position to return to from cur, moving cur to the
// forward history, or false when there is none.
func (h *History) GoBack(cur Entry) (Entry, bool) {
	if len(h.Back) == 0 {
		return Entry{}, false
	}
	to := h.Back[len(h.Back)-1]
	h.Back = h.Back[:len(h.Back)-1]
	h.Forward = appendBounded(h.Forward, cur, h.limit())
	return to, true
}

// GoForward undoes GoBack.
func (h *History) GoForward(cur Entry) (Entry, bool) {
	if len(h.Forward) == 0 {
		return Entry{}, false
	}
	to := h.Forward[len(h.Forward)-1]
	h.Forward = h.Forward[:len(h.Forward)-1]
	h.Back = appendBounded(h.Back, cur, h.limit())
	return to, true
}

// Forget drops every entry of a document, as one whose file is gone.
func (h *History) Forget(path string) {
	h.Back = without(h.Back, path)
	h.Forward = without(h.Forward, path)
}

// Clear empties the history.
func (h *History) Clear() { h.Back, h.Forward = nil, nil }

func appendBounded(s []Entry, e Entry, limit int) []Entry {
	s = append(s, e)
	if len(s) > limit {
		s = append(s[:0], s[len(s)-limit:]...)
	}
	return s
}

func without(s []Entry, path string) []Entry {
	out := s[:0]
	for _, e := range s {
		if e.Path != path {
			out = append(out, e)
		}
	}
	return out
}
