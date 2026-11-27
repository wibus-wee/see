package git

import (
	"bytes"
	"context"
	"os/exec"
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
	out, err := Command(ctx, dir, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory", "-z")
	if err != nil {
		return ig, err
	}
	if len(bytes.Trim(out, "\x00")) == 0 {
		return ig, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := proc.CommandContext(ctx, "git", "-c", "core.fsmonitor=false", "-C", dir, "check-ignore", "--stdin", "-v", "-z")
	// Not GIT_LITERAL_PATHSPECS, which check-ignore refuses.
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = bytes.NewReader(out)
	confirmed, err := cmd.Output()
	if err != nil && len(confirmed) == 0 {
		// check-ignore exits 1 when nothing is ignored.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return ig, nil
		}
		return ig, err
	}
	prefixOut, prefixErr := Command(ctx, dir, "rev-parse", "--show-prefix")
	if prefixErr != nil {
		return ig, prefixErr
	}
	prefix := strings.TrimSpace(string(prefixOut))
	fields := strings.Split(string(confirmed), "\x00")
	for i := 0; i+3 < len(fields); i += 4 {
		source, p := fields[i], fields[i+3]
		if p == "" || p == "./" || !useGlobal && isGlobalIgnoreSource(source) || !useParent && isParentIgnoreSource(source, prefix) {
			continue
		}
		if strings.HasSuffix(p, "/") {
			ig.dirs[strings.TrimSuffix(p, "/")] = true
		} else {
			ig.files[p] = true
		}
	}
	return ig, nil
}

func isGlobalIgnoreSource(source string) bool {
	return source != ".git/info/exclude" && !strings.HasSuffix(source, ".gitignore")
}

func isParentIgnoreSource(source, rootPrefix string) bool {
	if source == ".git/info/exclude" || !strings.HasSuffix(source, ".gitignore") {
		return false
	}
	return rootPrefix != "" && !strings.HasPrefix(source, rootPrefix)
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
