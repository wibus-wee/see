package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileAtCommit(t *testing.T) {
	root, run := newRepo(t)
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("old.txt", "first\n")
	run("add", ".")
	run("commit", "-qm", "first")
	first := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("mv", "old.txt", "new.txt")
	run("commit", "-qm", "rename")
	rename := strings.TrimSpace(run("rev-parse", "HEAD"))
	write("new.txt", "latest\n")
	run("add", ".")
	run("commit", "-qm", "latest")
	latest := strings.TrimSpace(run("rev-parse", "HEAD"))
	for _, tt := range []struct{ hash, want string }{{first, "first\n"}, {rename, "first\n"}, {latest, "latest\n"}} {
		got, err := FileAtCommit(context.Background(), root, "new.txt", tt.hash)
		if err != nil || string(got) != tt.want {
			t.Fatalf("%s: %q, %v", tt.hash, got, err)
		}
	}
	if _, err := FileAtCommit(context.Background(), root, "missing.txt", latest); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := FileAtCommit(context.Background(), root, "new.txt", "invalid"); err == nil {
		t.Fatal("invalid revision accepted")
	}
}
