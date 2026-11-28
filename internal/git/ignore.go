package git

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"see/internal/proc"
)

// Ignored is what .gitignore and the other exclude files leave out of a
// directory of a repository: folders whole, and files one by one.
type Ignored struct {
	dirs, files map[string]bool // slash-separated, relative to the directory
}

// ReadIgnored lists what git ignores under dir, relative to it. Git
// reports a folder holding only ignored files as ignored itself; asking
// check-ignore about each answer keeps those out. Outside a repository,
// or without git, it ignores nothing.
func ReadIgnored(ctx context.Context, dir string) (*Ignored, error) {
	return ReadIgnoredSources(ctx, dir, true, true)
}

// ReadIgnoredSources is ReadIgnored with global and parent ignore files independently controlled.
func ReadIgnoredSources(ctx context.Context, dir string, useGlobal, useParent bool) (*Ignored, error) {
	ig := &Ignored{dirs: map[string]bool{}, files: map[string]bool{}}
	out, err := ignoreSourceCommand(ctx, dir, nil, useGlobal, useParent, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return ig, err
	}
	if len(bytes.Trim(out, "\x00")) == 0 {
		return ig, nil
	}
	confirmed, err := ignoreSourceCommand(ctx, dir, out, useGlobal, useParent, "check-ignore", "--stdin", "-z")
	if err != nil && len(confirmed) == 0 {
		// check-ignore exits 1 when nothing is ignored.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return ig, nil
		}
		return ig, err
	}
	for _, p := range strings.Split(string(confirmed), "\x00") {
		switch {
		case p == "" || p == "./":
		case strings.HasSuffix(p, "/"):
			ig.dirs[strings.TrimSuffix(p, "/")] = true
		default:
			ig.files[p] = true
		}
	}
	return ig, nil
}

func ignoreSourceCommand(ctx context.Context, dir string, input []byte, useGlobal, useParent bool, args ...string) ([]byte, error) {
	gitArgs := []string{"-c", "core.fsmonitor=false"}
	if !useGlobal {
		gitArgs = append(gitArgs, "-c", "core.excludesFile=")
	}
	if useParent {
		gitArgs = append(gitArgs, "-C", dir)
	} else {
		gitDirOut, err := Command(ctx, dir, "rev-parse", "--git-dir")
		if err != nil {
			return nil, err
		}
		gitDir := strings.TrimSpace(string(gitDirOut))
		if !filepath.IsAbs(gitDir) {
			gitDir = filepath.Join(dir, gitDir)
		}
		gitArgs = append(gitArgs, "--git-dir", gitDir, "--work-tree", dir)
	}
	gitArgs = append(gitArgs, args...)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := proc.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat")
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	return cmd.Output()
}

// Dir reports whether git ignores the folder rel, or one it is in.
func (ig *Ignored) Dir(rel string) bool {
	if ig == nil {
		return false
	}
	for p := rel; p != "" && p != "."; {
		if ig.dirs[p] {
			return true
		}
		i := strings.LastIndexByte(p, '/')
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return false
}

// Has reports whether git ignores rel, a file or a folder, itself or
// through a folder it is in.
func (ig *Ignored) Has(rel string) bool {
	if ig == nil {
		return false
	}
	return ig.files[rel] || ig.Dir(rel)
}
