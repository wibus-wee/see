package git

import (
	"context"
	"testing"
)

func TestCommitSignaturePresent(t *testing.T) {
	if !(CommitSignature{Key: "3C1F165A4275D886"}).Present() {
		t.Fatal("a key is not present")
	}
	if (CommitSignature{}).Present() {
		t.Fatal("an empty signature is present")
	}
}

func TestShowReadsAnUnsignedCommit(t *testing.T) {
	root, run := newRepo(t)
	write(t, root, "a", "one\n")
	run("add", "a")
	run("commit", "-qm", "first")

	commits, err := Log(context.Background(), root, false, 0, 1)
	if err != nil || len(commits) != 1 {
		t.Fatalf("log: %+v %v", commits, err)
	}
	d, err := Show(context.Background(), root, commits[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if d.Body != "first" || d.Signature.Present() {
		t.Fatalf("details %+v", d)
	}
}
