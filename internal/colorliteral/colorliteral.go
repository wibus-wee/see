package colorliteral

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Color struct {
	R, G, B, A uint8
}

type Literal struct {
	Start, End int
	Raw        string
	Color      Color
}

// Bound malformed function candidates so repeated prefixes cannot rescan a document.
const maxFunctionLiteralBytes = 256

func Parse(src string) []Literal {
	return ParseLimit(src, 0)
}

func ParseLimit(src string, limit int) []Literal {
	var literals []Literal
	for i := 0; i < len(src); {
		if limit > 0 && len(literals) >= limit {
			return literals
		}
		if src[i] == '#' {
			if literal, end, ok := parseHex(src, i); ok {
				literals = append(literals, literal)
				i = end
				continue
			}
		}
		if isFunctionLead(src[i]) {
			if literal, end, ok := parseFunction(src, i); ok {
				literals = append(literals, literal)
				i = end
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(src[i:])
		if size < 1 {
			size = 1
		}
		i += size
	}
	return literals
}

func parseHex(src string, start int) (Literal, int, bool) {
	if hasNameRuneBefore(src, start) {
		return Literal{}, 0, false
	}
	end := start + 1
	for end < len(src) && isHex(src[end]) {
		end++
	}
	if hasNameRuneAfter(src, end) {
		return Literal{}, 0, false
	}
	digits := src[start+1 : end]
	var color Color
	switch len(digits) {
	case 3:
		color = Color{expandNibble(digits[0]), expandNibble(digits[1]), expandNibble(digits[2]), 255}
	case 4:
		color = Color{expandNibble(digits[0]), expandNibble(digits[1]), expandNibble(digits[2]), expandNibble(digits[3])}
	case 6:
		color = Color{byteAt(digits, 0), byteAt(digits, 2), byteAt(digits, 4), 255}
	case 8:
		color = Color{byteAt(digits, 0), byteAt(digits, 2), byteAt(digits, 4), byteAt(digits, 6)}
	default:
		return Literal{}, 0, false
	}
	return Literal{Start: start, End: end, Raw: src[start:end], Color: color}, end, true
}

func parseFunction(src string, start int) (Literal, int, bool) {
	if hasNameRuneBefore(src, start) {
		return Literal{}, 0, false
	}
	rest := src[start:]
	nameLen, hsl := 0, false
	for _, name := range []string{"rgba", "rgb", "hsla", "hsl"} {
		if len(rest) > len(name) && strings.EqualFold(rest[:len(name)], name) && rest[len(name)] == '(' {
			nameLen, hsl = len(name), strings.HasPrefix(name, "hsl")
			break
		}
	}
	if nameLen == 0 {
		return Literal{}, 0, false
	}
	argsStart := start + nameLen + 1
	scanEnd := start + min(len(src)-start, maxFunctionLiteralBytes)
	close := strings.IndexByte(src[argsStart:scanEnd], ')')
	if close < 0 {
		return Literal{}, 0, false
	}
	end := argsStart + close + 1
	if hasNameRuneAfter(src, end) {
		return Literal{}, 0, false
	}
	color, ok := parseComponents(src[argsStart:end-1], hsl)
	if !ok {
		return Literal{}, 0, false
	}
	return Literal{Start: start, End: end, Raw: src[start:end], Color: color}, end, true
}

func parseComponents(args string, hsl bool) (Color, bool) {
	var channels []string
	var alpha string
	alphaPresent := false
	if strings.Contains(args, ",") {
		if strings.Contains(args, "/") {
			return Color{}, false
		}
		parts := strings.Split(args, ",")
		if len(parts) != 3 && len(parts) != 4 {
			return Color{}, false
		}
		channels = parts[:3]
		if len(parts) == 4 {
			alpha = parts[3]
			alphaPresent = true
		}
	} else {
		parts := strings.Split(args, "/")
		if len(parts) > 2 {
			return Color{}, false
		}
		channels = strings.Fields(parts[0])
		if len(channels) != 3 {
			return Color{}, false
		}
		if len(parts) == 2 {
			alpha = parts[1]
			alphaPresent = true
		}
	}
	for i := range channels {
		channels[i] = strings.TrimSpace(channels[i])
	}
	alpha = strings.TrimSpace(alpha)
	var color Color
	if hsl {
		hue, ok := parseHue(channels[0])
		if !ok {
			return Color{}, false
		}
		saturation, ok := parsePercent(channels[1])
		if !ok {
			return Color{}, false
		}
		lightness, ok := parsePercent(channels[2])
		if !ok {
			return Color{}, false
		}
		color = hslColor(hue, saturation, lightness)
	} else {
		first, percent, ok := parseRGBChannel(channels[0])
		if !ok {
			return Color{}, false
		}
		second, secondPercent, ok := parseRGBChannel(channels[1])
		if !ok || secondPercent != percent {
			return Color{}, false
		}
		third, thirdPercent, ok := parseRGBChannel(channels[2])
		if !ok || thirdPercent != percent {
			return Color{}, false
		}
		color = Color{byteFromUnit(first), byteFromUnit(second), byteFromUnit(third), 255}
	}
	if alphaPresent {
		a, ok := parseAlpha(alpha)
		if !ok {
			return Color{}, false
		}
		color.A = byteFromUnit(a)
	}
	return color, true
}

func parseRGBChannel(s string) (float64, bool, bool) {
	percent := strings.HasSuffix(s, "%")
	if percent {
		s = strings.TrimSuffix(s, "%")
	}
	v, ok := parseCSSNumber(s)
	if !ok {
		return 0, false, false
	}
	if percent {
		if v < 0 || v > 100 {
			return 0, false, false
		}
		return v / 100, true, true
	}
	if v < 0 || v > 255 {
		return 0, false, false
	}
	return v / 255, false, true
}

func parsePercent(s string) (float64, bool) {
	if !strings.HasSuffix(s, "%") {
		return 0, false
	}
	v, ok := parseCSSNumber(strings.TrimSuffix(s, "%"))
	if !ok || v < 0 || v > 100 {
		return 0, false
	}
	return v / 100, true
}

func parseAlpha(s string) (float64, bool) {
	if strings.HasSuffix(s, "%") {
		v, ok := parseCSSNumber(strings.TrimSuffix(s, "%"))
		if !ok || v < 0 || v > 100 {
			return 0, false
		}
		return v / 100, true
	}
	v, ok := parseCSSNumber(s)
	return v, ok && v >= 0 && v <= 1
}

func parseHue(s string) (float64, bool) {
	unit := "deg"
	for _, suffix := range []string{"turn", "grad", "deg", "rad"} {
		if strings.HasSuffix(strings.ToLower(s), suffix) {
			unit = suffix
			s = s[:len(s)-len(suffix)]
			break
		}
	}
	v, ok := parseCSSNumber(s)
	if !ok {
		return 0, false
	}
	switch unit {
	case "turn":
		v *= 360
	case "grad":
		v *= 0.9
	case "rad":
		v *= 180 / math.Pi
	}
	if !isFinite(v) {
		return 0, false
	}
	v = math.Mod(v, 360)
	if v < 0 {
		v += 360
	}
	return v / 360, true
}

func parseCSSNumber(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	i := 0
	if s[i] == '+' || s[i] == '-' {
		i++
		if i == len(s) {
			return 0, false
		}
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		fractionStart := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
		if i == fractionStart && digits == 0 {
			return 0, false
		}
	} else if digits == 0 {
		return 0, false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		exponentStart := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == exponentStart {
			return 0, false
		}
	}
	if i != len(s) {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil && isFinite(v)
}

func hslColor(h, s, l float64) Color {
	if s == 0 {
		v := byteFromUnit(l)
		return Color{v, v, v, 255}
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	return Color{
		byteFromUnit(hueChannel(p, q, h+1.0/3)),
		byteFromUnit(hueChannel(p, q, h)),
		byteFromUnit(hueChannel(p, q, h-1.0/3)),
		255,
	}
}

func hueChannel(p, q, t float64) float64 {
	if t < 0 {
		t++
	}
	if t > 1 {
		t--
	}
	switch {
	case t < 1.0/6:
		return p + (q-p)*6*t
	case t < 1.0/2:
		return q
	case t < 2.0/3:
		return p + (q-p)*(2.0/3-t)*6
	default:
		return p
	}
}

func byteFromUnit(v float64) uint8 {
	return uint8(math.Round(v * 255))
}

func byteAt(s string, i int) uint8 {
	hi, lo := hexNibble(s[i]), hexNibble(s[i+1])
	return hi<<4 | lo
}

func expandNibble(b byte) uint8 {
	v := hexNibble(b)
	return v<<4 | v
}

func hexNibble(b byte) uint8 {
	if b >= '0' && b <= '9' {
		return b - '0'
	}
	if b >= 'a' && b <= 'f' {
		return b - 'a' + 10
	}
	return b - 'A' + 10
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}

func isFunctionLead(b byte) bool {
	return b == 'r' || b == 'R' || b == 'h' || b == 'H'
}

func hasNameRuneBefore(s string, i int) bool {
	if i == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return isNameRune(r) || r == '.'
}

func hasNameRuneAfter(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isNameRune(r)
}

func isNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
