// Package hexview reads and formats fixed-size rows of file bytes.
package hexview

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const RowBytes = 16
const PageBytes = 64 << 10

// ReadPage reads one bounded page at offset without retaining an open file.
func ReadPage(path string, offset, size int64) ([]byte, error) {
	if offset < 0 || offset%PageBytes != 0 || size < 0 || offset >= size {
		return nil, fmt.Errorf("invalid page offset")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	n := min(int64(PageBytes), size-offset)
	data := make([]byte, int(n))
	got, err := f.ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if got != len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	return data, nil
}

// Rows returns the number of rows needed to show size bytes.
func Rows(size int64) int64 {
	if size <= 0 {
		return 0
	}
	return 1 + (size-1)/RowBytes
}

// Format renders one row with a hex offset and an ASCII column.
func Format(offset int64, data []byte) string {
	if len(data) > RowBytes {
		data = data[:RowBytes]
	}
	var b strings.Builder
	b.Grow(90)
	fmt.Fprintf(&b, "%016X  ", offset)
	const digits = "0123456789ABCDEF"
	for i := 0; i < RowBytes; i++ {
		if i < len(data) {
			b.WriteByte(digits[data[i]>>4])
			b.WriteByte(digits[data[i]&15])
		} else {
			b.WriteString("  ")
		}
		b.WriteByte(' ')
		if i == 7 {
			b.WriteByte(' ')
		}
	}
	b.WriteString(" |")
	for _, ch := range data {
		if ch >= 32 && ch <= 126 {
			b.WriteByte(ch)
		} else {
			b.WriteByte('.')
		}
	}
	b.WriteByte('|')
	return b.String()
}
