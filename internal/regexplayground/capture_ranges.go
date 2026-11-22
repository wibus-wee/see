package regexplayground

import (
	"container/heap"
	"sort"
)

type CaptureSpan struct {
	Start, End int
	Number     int
}

type captureSpanEvent struct {
	position int
	span     CaptureSpan
	starts   bool
}

type captureSpanHeap []CaptureSpan

func (h captureSpanHeap) Len() int { return len(h) }
func (h captureSpanHeap) Less(i, j int) bool {
	iSize, jSize := h[i].End-h[i].Start, h[j].End-h[j].Start
	if iSize != jSize {
		return iSize < jSize
	}
	return h[i].Number < h[j].Number
}
func (h captureSpanHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *captureSpanHeap) Push(value any) {
	*h = append(*h, value.(CaptureSpan))
}
func (h *captureSpanHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	*h = old[:last]
	return value
}

// CaptureHighlights divides matched capture ranges into non-overlapping spans.
// The smallest enclosing capture owns overlap; group number breaks ties.
func CaptureHighlights(captures []Capture) []CaptureSpan {
	var events []captureSpanEvent
	for _, capture := range captures {
		if capture.Number <= 0 || !capture.Matched || capture.Start < 0 || capture.End <= capture.Start {
			continue
		}
		span := CaptureSpan{Start: capture.Start, End: capture.End, Number: capture.Number}
		events = append(events,
			captureSpanEvent{position: span.Start, span: span, starts: true},
			captureSpanEvent{position: span.End, span: span},
		)
	}
	if len(events) == 0 {
		return nil
	}
	sort.Slice(events, func(i, j int) bool { return events[i].position < events[j].position })

	active := make(map[int]bool, len(events)/2)
	var spans captureSpanHeap
	var out []CaptureSpan
	for i := 0; i < len(events); {
		position := events[i].position
		j := i
		for j < len(events) && events[j].position == position {
			event := events[j]
			if event.starts {
				active[event.span.Number] = true
				heap.Push(&spans, event.span)
			} else {
				active[event.span.Number] = false
			}
			j++
		}
		for spans.Len() > 0 && !active[spans[0].Number] {
			heap.Pop(&spans)
		}
		if j < len(events) && events[j].position > position && spans.Len() > 0 {
			winner := spans[0]
			if len(out) > 0 && out[len(out)-1].Number == winner.Number && out[len(out)-1].End == position {
				out[len(out)-1].End = events[j].position
			} else {
				out = append(out, CaptureSpan{Start: position, End: events[j].position, Number: winner.Number})
			}
		}
		i = j
	}
	return out
}
