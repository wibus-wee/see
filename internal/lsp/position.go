package lsp

import (
	"unicode/utf8"
)

// Position encodings of LSP 3.17 (PositionEncodingKind).
const (
	UTF8  = "utf-8"
	UTF16 = "utf-16"
	UTF32 = "utf-32"
)

// ColumnToLSP converts a byte offset into line (a line's text without its
// terminator) to a character offset in the encoding enc, UTF-16 when enc
// is empty or unknown. A byte column inside a character counts from that
// character's start; one past the end is clamped to the line's length.
func ColumnToLSP(line string, byteCol int, enc string) int {
	if byteCol <= 0 {
		return 0
	}
	if byteCol > len(line) {
		byteCol = len(line)
	}
	if enc == UTF8 {
		// Back up to the start of a character.
		for byteCol > 0 && byteCol < len(line) && !utf8.RuneStart(line[byteCol]) {
			byteCol--
		}
		return byteCol
	}
	n := 0
	for i := 0; i < byteCol; {
		r, size := utf8.DecodeRuneInString(line[i:])
		if i+size > byteCol {
			break // byteCol is inside this character
		}
		n += units(r, enc)
		i += size
	}
	return n
}

// ColumnFromLSP converts a character offset in encoding enc into a byte
// offset into line. Offsets past the end of the line clamp to its length,
// as the protocol asks; one inside a character (the second half of a
// surrogate pair) lands on that character's start.
func ColumnFromLSP(line string, char int, enc string) int {
	if char <= 0 {
		return 0
	}
	if enc == UTF8 {
		if char >= len(line) {
			return len(line)
		}
		for char > 0 && !utf8.RuneStart(line[char]) {
			char--
		}
		return char
	}
	n := 0
	for i := 0; i < len(line); {
		r, size := utf8.DecodeRuneInString(line[i:])
		u := units(r, enc)
		if n+u > char {
			return i
		}
		n += u
		i += size
		if n == char {
			return i
		}
	}
	return len(line)
}

// units is how many code units of enc rune r takes.
func units(r rune, enc string) int {
	if enc == UTF32 {
		return 1
	}
	if r >= 0x10000 && r <= utf8.MaxRune {
		return 2 // a surrogate pair
	}
	return 1
}

// NegotiatedEncoding picks the encoding from a server's
// capabilities.positionEncoding, UTF-16 when absent, as LSP 3.17 says.
func NegotiatedEncoding(server string) string {
	switch server {
	case UTF8, UTF32:
		return server
	}
	return UTF16
}
