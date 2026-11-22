package git

import (
	"context"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPreviewBranchReadsCommittedChanges(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	write(t, root, "modified.txt", "before\n")
	write(t, root, "deleted.txt", "gone\n")
	write(t, root, "rename source.txt", "rename this file\nwith enough text to detect it\n")
	run("add", ".")
	run("commit", "-qm", "base")
	head := strings.TrimSpace(run("rev-parse", "HEAD"))
	branch := "topic/--end-of-options"
	run("switch", "-qc", branch)
	write(t, root, "modified.txt", "after\n")
	run("rm", "deleted.txt")
	run("mv", "rename source.txt", "rename destination.txt")
	write(t, root, "--leading-name.txt", "option-looking path\n")
	write(t, root, "with;punctuation.txt", "punctuation\n")
	run("add", "--all")
	run("commit", "-qm", "files changed")
	run("commit", "--allow-empty", "-qm", "middle commit")
	run("commit", "--allow-empty", "-qm", "latest commit")
	target := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("switch", "-q", "main")

	bs, err := Branches(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var b Branch
	for _, candidate := range bs {
		if candidate.Name == branch {
			b = candidate
		}
	}
	preview, err := PreviewBranch(ctx, root, b, bs)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TargetRef != "refs/heads/"+branch || preview.TargetHash != target {
		t.Fatalf("target ref/hash = %q/%q, want %q/%q", preview.TargetRef, preview.TargetHash, "refs/heads/"+branch, target)
	}
	if got := []string{preview.Commits[0].Subject, preview.Commits[1].Subject, preview.Commits[2].Subject}; !slices.Equal(got, []string{"latest commit", "middle commit", "files changed"}) {
		t.Fatalf("recent commits = %v", got)
	}
	changes := map[string]BranchFileChange{}
	for _, change := range preview.Changes {
		changes[change.Path] = change
	}
	for path, status := range map[string]byte{
		"modified.txt":           'M',
		"deleted.txt":            'D',
		"rename destination.txt": 'R',
		"--leading-name.txt":     'A',
		"with;punctuation.txt":   'A',
	} {
		if got := changes[path].Status; got != status {
			t.Errorf("%q status = %q, want %q (all changes: %+v)", path, got, status, changes)
		}
	}
	if got := changes["rename destination.txt"].OldPath; got != "rename source.txt" {
		t.Errorf("rename source = %q", got)
	}
	if got := Head(ctx, root); got != head {
		t.Errorf("preview changed HEAD to %q, want %q", got, head)
	}
}

func TestPreviewBranchHandlesHostilePathAndRemoteLocalCollision(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	write(t, root, "base.txt", "base\n")
	write(t, root, "local.txt", "base local\n")
	run("add", ".")
	run("commit", "-qm", "base")
	run("switch", "-qc", "topic")
	write(t, root, "local.txt", "local target\n")
	run("add", "--", "local.txt")
	run("commit", "-qm", "local target")
	localHash := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("switch", "-q", "main")
	run("switch", "-q", "--detach", "HEAD")
	remotePath := "odd;--path.txt"
	if runtime.GOOS != "windows" {
		remotePath = "odd\tname\n--path.txt"
	}
	write(t, root, remotePath, "remote only\n")
	run("add", "--", remotePath)
	run("commit", "-qm", "remote target")
	remoteHash := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("switch", "-q", "main")
	run("update-ref", "refs/remotes/origin/topic", remoteHash)
	if localHash == remoteHash {
		t.Fatal("local and remote targets unexpectedly point to the same commit")
	}

	bs, err := Branches(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewBranch(ctx, root, Branch{Name: "origin/topic", Remote: true}, bs)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TargetRef != "refs/heads/topic" || preview.TargetHash != localHash {
		t.Fatalf("remote preview points to %q at %q, want local switch target %q at %q", preview.TargetRef, preview.TargetHash, "refs/heads/topic", localHash)
	}
	if len(preview.Commits) == 0 || preview.Commits[0].Subject != "local target" {
		t.Fatalf("remote collision preview commits = %+v, want local target", preview.Commits)
	}
	if len(preview.Changes) != 1 || preview.Changes[0].Path != "local.txt" || preview.Changes[0].Status != 'M' {
		t.Fatalf("remote collision preview changes = %+v, want local.txt modified", preview.Changes)
	}

	parsed, err := parseBranchFileChanges([]byte("A\x00" + remotePath + "\x00"))
	if err != nil || len(parsed) != 1 || parsed[0].Path != remotePath || parsed[0].Status != 'A' {
		t.Fatalf("NUL-delimited hostile path = %+v, %v", parsed, err)
	}
	if _, err := PreviewBranch(ctx, filepath.Join(root, "missing"), Branch{Name: "topic"}, nil); err == nil {
		t.Fatal("preview outside a repository succeeded")
	}
}

func TestPreviewBranchUsesRemoteWhenNoLocalBranchExists(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	write(t, root, "base.txt", "base\n")
	run("add", ".")
	run("commit", "-qm", "base")
	run("switch", "-q", "--detach", "HEAD")
	write(t, root, "remote-only.txt", "remote change\n")
	run("add", "--", "remote-only.txt")
	run("commit", "-qm", "remote-only target")
	remoteHash := strings.TrimSpace(run("rev-parse", "HEAD"))
	run("switch", "-q", "main")
	run("update-ref", "refs/remotes/origin/topic", remoteHash)

	bs, err := Branches(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(bs, func(b Branch) bool { return !b.Remote && b.Name == "topic" }) {
		t.Fatal("fixture unexpectedly has a local topic branch")
	}
	var remote Branch
	for _, b := range bs {
		if b.Remote && b.Name == "origin/topic" {
			remote = b
			break
		}
	}
	if !remote.Remote {
		t.Fatalf("remote branch missing: %+v", bs)
	}
	preview, err := PreviewBranch(ctx, root, remote, bs)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TargetRef != "refs/remotes/origin/topic" || preview.TargetHash != remoteHash {
		t.Fatalf("remote-only preview points to %q at %q, want remote target %q", preview.TargetRef, preview.TargetHash, remoteHash)
	}
	if len(preview.Commits) == 0 || preview.Commits[0].Subject != "remote-only target" {
		t.Fatalf("remote-only preview commits = %+v", preview.Commits)
	}
	if len(preview.Changes) != 1 || preview.Changes[0].Path != "remote-only.txt" || preview.Changes[0].Status != 'A' {
		t.Fatalf("remote-only preview changes = %+v", preview.Changes)
	}
}

func TestPreviewBranchWithNoChanges(t *testing.T) {
	root, run := newRepo(t)
	ctx := context.Background()
	run("commit", "--allow-empty", "-qm", "first")
	bs, err := Branches(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewBranch(ctx, root, Branch{Name: "main"}, bs)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Changes) != 0 || len(preview.Commits) != 1 || preview.Commits[0].Subject != "first" {
		t.Fatalf("unchanged branch preview = %+v", preview)
	}
}
