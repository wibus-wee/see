package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type BlameLine struct {
	Hash, Short, Author, Email, Subject string
	Date                                time.Time
	Uncommitted                         bool
}

func Blame(ctx context.Context, root, rel string, contents []byte) ([]BlameLine, error) {
	if contents == nil {
		contents = []byte{}
	}
	out, err := commandInput(ctx, root, contents, "blame", "--porcelain", "--contents", "-", "--", rel)
	if err != nil {
		return nil, err
	}
	return parseBlame(out)
}

func parseBlame(out []byte) ([]BlameLine, error) {
	var lines []BlameLine
	commits := map[string]BlameLine{}
	var current BlameLine
	line := -1
	for text := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(text, "\t") {
			if line < 0 {
				return nil, fmt.Errorf("invalid blame line")
			}
			for len(lines) <= line {
				lines = append(lines, BlameLine{})
			}
			lines[line] = current
			commits[current.Hash] = current
			line = -1
			continue
		}
		key, value, _ := strings.Cut(text, " ")
		if len(key) == 40 || len(key) == 64 {
			if _, err := strconv.ParseUint(key[:16], 16, 64); err == nil {
				fields := strings.Fields(value)
				if len(fields) < 2 {
					return nil, fmt.Errorf("invalid blame header")
				}
				n, err := strconv.Atoi(fields[1])
				if err != nil || n <= 0 {
					return nil, fmt.Errorf("invalid blame line number")
				}
				line = n - 1
				current = commits[key]
				current.Hash, current.Short = key, key[:min(7, len(key))]
				current.Uncommitted = strings.Trim(key, "0") == ""
				continue
			}
		}
		switch key {
		case "author":
			current.Author = value
		case "author-mail":
			current.Email = strings.Trim(value, "<>")
		case "author-time":
			sec, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid blame author time: %w", err)
			}
			current.Date = time.Unix(sec, 0)
		case "summary":
			current.Subject = value
		}
	}
	return lines, nil
}
