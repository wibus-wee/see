// Package hexview reads and formats fixed-size rows of file bytes.
package hexview

import (
	"fmt"
	"strings"
)

const RowBytes = 16
const PageBytes = 64 << 10

// PageLen is the number of bytes the page starting at offset holds in a
// file of size: PageBytes for every page but the final one, which ends
// at the file's end. Callers read pages through a rangeio.Session so
// every page of one view comes from the same version of the file.
func PageLen(offset, size int64) int64 {
	return min(int64(PageBytes), size-offset)
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
