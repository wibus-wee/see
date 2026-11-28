package hexview

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"see/internal/rangeio"
)

// ErrNotFound reports a search that scanned to the end of the file.
var ErrNotFound = errors.New("no match before the end of the file")

// Pattern parses a find query: hex bytes after "hex:", UTF-8 text
// otherwise.
func Pattern(query string) ([]byte, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if rest, ok := strings.CutPrefix(strings.ToLower(query), "hex:"); ok {
		compact := strings.Join(strings.Fields(rest), "")
		data, err := hex.DecodeString(compact)
		if err != nil {
			return nil, fmt.Errorf("parsing hex bytes: %w", err)
		}
		return data, nil
	}
	return []byte(query), nil
}

// Find scans the pinned version of the file for the first occurrence of
// pattern at or after start. The scan streams in order, so live memory
// stays bounded by one chunk plus the pattern no matter the file, and it
// fails with rangeio.ErrChanged rather than mixing bytes of two versions
// into one match position. An empty pattern matches at start; a negative
// start is an error, as Scan and ReadAt contract.
func Find(ctx context.Context, s *rangeio.Session, start int64, pattern []byte) (int64, error) {
	if start < 0 {
		return -1, fmt.Errorf("rangeio: negative start %d", start)
	}
	if len(pattern) == 0 {
		if start <= s.Size() {
			return start, nil
		}
		return -1, ErrNotFound
	}
	found := int64(-1)
	// Every position of the file is searched in the window of the chunk
	// its byte starts in, and a match spanning a chunk boundary starts
	// within the last len(pattern)-1 bytes of a chunk, so carrying that
	// tail into the next window covers every position exactly once. The
	// scan stops at the first hit, so overlapping windows never report
	// the same match twice.
	keep := len(pattern) - 1
	var tail []byte
	err := s.Scan(ctx, start, 0, func(off int64, data []byte) bool {
		window := data
		base := off
		if len(tail) > 0 {
			window = make([]byte, 0, len(tail)+len(data))
			window = append(window, tail...)
			window = append(window, data...)
			base = off - int64(len(tail))
		}
		if i := bytes.Index(window, pattern); i >= 0 {
			found = base + int64(i)
			return false
		}
		if keep > 0 {
			if len(window) > keep {
				window = window[len(window)-keep:]
			}
			tail = append([]byte(nil), window...)
		}
		return true
	})
	if err != nil {
		return -1, err
	}
	if found < 0 {
		return -1, ErrNotFound
	}
	return found, nil
}
