package git

import "strings"

// Message is a commit's message, split where git's conventions do: its
// subject on one line, its body, and the people its Co-authored-by
// trailers name.
type Message struct {
	Subject   string
	Body      string
	CoAuthors []Person
}

// ParseMessage splits a raw commit message (%B) at its first blank
// line and takes the Co-authored-by trailers out of the trailer block
// at its end. Other trailers stay in the body, as written.
func ParseMessage(raw, authorEmail string) Message {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimSpace(raw)

	subject, body := raw, ""
	if i := strings.Index(raw, "\n\n"); i >= 0 {
		subject, body = raw[:i], raw[i+2:]
	}
	msg := Message{Subject: strings.ReplaceAll(strings.TrimSpace(subject), "\n", " ")}
	body = strings.TrimSpace(body)
	if body == "" {
		return msg
	}

	lines := strings.Split(body, "\n")
	start := trailerBlockStart(lines)
	if start < 0 {
		msg.Body = body
		return msg
	}
	var coAuthors []string
	kept := make([]string, 0, len(lines))
	kept = append(kept, lines[:start]...)
	for _, line := range lines[start:] {
		key, value, ok := splitTrailer(line)
		if ok && strings.EqualFold(key, "Co-authored-by") {
			coAuthors = append(coAuthors, value)
			continue
		}
		kept = append(kept, line)
	}
	msg.Body = strings.TrimSpace(strings.Join(kept, "\n"))
	msg.CoAuthors = ParseCoAuthors(coAuthors, authorEmail)
	return msg
}

// trailerBlockStart is the index of the first line of the trailer
// block at the end of lines, or -1 when the last paragraph is not all
// trailers.
func trailerBlockStart(lines []string) int {
	start := len(lines)
	for start > 0 && lines[start-1] != "" {
		start--
	}
	if start == len(lines) {
		return -1
	}
	for _, line := range lines[start:] {
		if _, _, ok := splitTrailer(line); !ok {
			return -1
		}
	}
	return start
}

// splitTrailer parses a "Key: value" line, as git's trailers write
// them, and reports whether line is one.
func splitTrailer(line string) (key, value string, ok bool) {
	i := strings.Index(line, ": ")
	if i <= 0 {
		return "", "", false
	}
	key = line[:i]
	for _, r := range key {
		if !(r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return "", "", false
		}
	}
	return key, strings.TrimSpace(line[i+2:]), true
}
