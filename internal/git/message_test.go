package git

import (
	"slices"
	"strings"
	"testing"
)

func TestParseMessageSplitsSubjectBodyAndTrailers(t *testing.T) {
	raw := "fix: thing\nsecond line\n\nBody one.\n\nCo-authored-by: Ada <ada@x.org>\nSigned-off-by: Bob <bob@x.org>\n"
	msg := ParseMessage(raw, "me@x.org")
	if got, want := msg.Subject, "fix: thing second line"; got != want {
		t.Fatalf("subject %q, want %q", got, want)
	}
	if got, want := msg.Body, "Body one.\n\nSigned-off-by: Bob <bob@x.org>"; got != want {
		t.Fatalf("body %q, want %q", got, want)
	}
	if want := []Person{{Name: "Ada", Email: "ada@x.org"}}; !slices.Equal(msg.CoAuthors, want) {
		t.Fatalf("co-authors %+v, want %+v", msg.CoAuthors, want)
	}
}

func TestParseMessageWithoutBody(t *testing.T) {
	msg := ParseMessage("subject only\n", "me@x.org")
	if msg.Subject != "subject only" || msg.Body != "" || len(msg.CoAuthors) != 0 {
		t.Fatalf("message %+v", msg)
	}
}

func TestParseMessageNormalizesCRLF(t *testing.T) {
	msg := ParseMessage("subject\r\n\r\nbody\r\nmore\r\n", "me@x.org")
	if msg.Subject != "subject" || msg.Body != "body\nmore" {
		t.Fatalf("message %+v", msg)
	}
}

func TestParseMessageKeepsNonTrailerCoAuthoredByLines(t *testing.T) {
	raw := "subject\n\nCo-authored-by: not a trailer\n\nThe body\n"
	msg := ParseMessage(raw, "me@x.org")
	if msg.Body != "Co-authored-by: not a trailer\n\nThe body" || len(msg.CoAuthors) != 0 {
		t.Fatalf("message %+v", msg)
	}
}

func TestParseMessageTrailerBlockOnlyAtTheEnd(t *testing.T) {
	raw := "subject\n\nBody.\n\nCo-authored-by: Ada <ada@x.org>\n\nMore body.\n"
	msg := ParseMessage(raw, "me@x.org")
	if msg.Body != "Body.\n\nCo-authored-by: Ada <ada@x.org>\n\nMore body." || len(msg.CoAuthors) != 0 {
		t.Fatalf("message %+v", msg)
	}
}

func TestParseMessageDeduplicatesAndExcludesTheAuthor(t *testing.T) {
	raw := "subject\n\nCo-authored-by: Ada <ada@x.org>\nco-authored-by: Ada Again <ADA@x.org>\nCo-authored-by: Me <me@x.org>\nSigned-off-by: Bob <bob@x.org>\n"
	msg := ParseMessage(raw, "me@x.org")
	if want := []Person{{Name: "Ada", Email: "ada@x.org"}}; !slices.Equal(msg.CoAuthors, want) {
		t.Fatalf("co-authors %+v, want %+v", msg.CoAuthors, want)
	}
	if got, want := msg.Body, "Signed-off-by: Bob <bob@x.org>"; got != want {
		t.Fatalf("body %q, want %q", got, want)
	}
}

// A body with paragraphs and trailer-like lines between them keeps its
// prose; only the trailer block at its end is taken out.
func TestParseMessageKeepsBodyAroundTrailers(t *testing.T) {
	raw := "fix: previews\n\n* First change\n\nCo-Authored-By: Devin <devin@x.org>\n\n* Second change\n\nCo-authored-by: Devin <devin@x.org>\n"
	msg := ParseMessage(raw, "me@x.org")
	if !strings.Contains(msg.Body, "* First change") || !strings.Contains(msg.Body, "* Second change") {
		t.Fatalf("body lost its paragraphs: %q", msg.Body)
	}
	if strings.Contains(msg.Body, "Co-authored-by: Devin") {
		t.Fatalf("body kept the trailer block: %q", msg.Body)
	}
	if want := []Person{{Name: "Devin", Email: "devin@x.org"}}; !slices.Equal(msg.CoAuthors, want) {
		t.Fatalf("co-authors %+v, want %+v", msg.CoAuthors, want)
	}
}

func TestParseMessageOnlyCoAuthorsLeavesNoBody(t *testing.T) {
	msg := ParseMessage("subject\n\nCo-authored-by: Ada <ada@x.org>\n", "me@x.org")
	if msg.Body != "" || len(msg.CoAuthors) != 1 {
		t.Fatalf("message %+v", msg)
	}
}
