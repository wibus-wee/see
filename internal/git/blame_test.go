package git

import (
	"context"
	"testing"
)

func TestBlameBufferContents(t *testing.T) {
	root, run := newRepo(t)
	write(t, root, "a", "one\ntwo\nthree\n")
	run("add", "a")
	run("commit", "-qm", "first")
	hash := Head(context.Background(), root)
	lines, err := Blame(context.Background(), root, "a", []byte("one\nchanged\nthree\n"))
	if err != nil || len(lines) != 3 {
		t.Fatalf("blame: %+v, %v", lines, err)
	}
	for _, i := range []int{0, 2} {
		b := lines[i]
		if b.Hash != hash || b.Short != hash[:7] || b.Author != "See" || b.Email != "see@example.com" || b.Subject != "first" || b.Date.IsZero() || b.Uncommitted {
			t.Fatalf("line %d: %+v", i, b)
		}
	}
	if !lines[1].Uncommitted {
		t.Fatalf("edited line: %+v", lines[1])
	}
	lines, err = Blame(context.Background(), root, "a", []byte{})
	if err != nil || len(lines) != 0 {
		t.Fatalf("empty buffer: %+v, %v", lines, err)
	}
	write(t, root, "untracked", "new\n")
	if lines, err = Blame(context.Background(), root, "untracked", []byte("new\n")); err == nil || len(lines) != 0 {
		t.Fatalf("untracked: %+v, %v", lines, err)
	}
}

func TestParseBlameInvalid(t *testing.T) {
	if _, err := parseBlame([]byte("\tmissing header\n")); err == nil {
		t.Fatal("accepted missing header")
	}
}
