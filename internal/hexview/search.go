package hexview

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
)

var ErrNotFound = errors.New("no match")

// Pattern interprets a hex: prefix as hexadecimal bytes; other input is UTF-8 text.
func Pattern(query string) ([]byte, error) {
	if strings.HasPrefix(strings.ToLower(query), "hex:") {
		s := strings.Join(strings.Fields(query[4:]), "")
		return hex.DecodeString(s)
	}
	return []byte(query), nil
}

// Find searches [start, size) in bounded chunks, including matches across chunks.
func Find(ctx context.Context, path string, size, start int64, pattern []byte) (int64, error) {
	if len(pattern) == 0 || len(pattern) > PageBytes || start < 0 || start > size {
		return -1, errors.New("invalid search")
	}
	f, err := os.Open(path)
	if err != nil {
		return -1, err
	}
	defer f.Close()
	buf := make([]byte, PageBytes+len(pattern)-1)
	pos, carry := start, 0
	for pos < size {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		n := int(min(int64(PageBytes), size-pos))
		got, err := f.ReadAt(buf[carry:carry+n], pos)
		if err != nil && err != io.EOF {
			return -1, err
		}
		if got != n {
			return -1, io.ErrUnexpectedEOF
		}
		total := carry + got
		if i := bytes.Index(buf[:total], pattern); i >= 0 {
			return pos - int64(carry) + int64(i), nil
		}
		carry = min(len(pattern)-1, total)
		copy(buf[:carry], buf[total-carry:total])
		pos += int64(got)
	}
	return -1, ErrNotFound
}
