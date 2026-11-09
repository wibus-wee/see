package git

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Network runs git in dir as Command does, for a command that talks to a
// remote: it gives up after five minutes rather than thirty seconds.
func Network(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	args = append([]string{"-c", "core.fsmonitor=false", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_LITERAL_PATHSPECS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Tracking is the branch HEAD's branch tracks, and how many commits HEAD
// has that it has not (ahead) and it has that HEAD has not (behind); ""
// without one.
func Tracking(ctx context.Context, root string) (upstream string, ahead, behind int) {
	out, err := Command(ctx, root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", 0, 0
	}
	upstream = strings.TrimSpace(string(out))
	out, err = Command(ctx, root, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return upstream, 0, 0
	}
	if f := strings.Fields(string(out)); len(f) == 2 {
		ahead, _ = strconv.Atoi(f[0])
		behind, _ = strconv.Atoi(f[1])
	}
	return upstream, ahead, behind
}

// Refs includes names as well as targets: a push, tag or branch switch
// can change history decorations without changing HEAD.
func Refs(ctx context.Context, root string) (string, error) {
	out, err := Command(ctx, root, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)", "refs/heads", "refs/remotes", "refs/tags")
	return string(out), err
}

// Remotes lists the repository's remotes, origin first.
func Remotes(ctx context.Context, root string) []string {
	out, err := Command(ctx, root, "remote")
	if err != nil {
		return nil
	}
	var remotes []string
	for _, r := range strings.Fields(string(out)) {
		if r == "origin" {
			remotes = append([]string{r}, remotes...)
		} else {
			remotes = append(remotes, r)
		}
	}
	return remotes
}

// WebURL is the web page of a remote's repository, as GitHub, GitLab and
// most hosts serve it, from its fetch URL: git@github.com:o/r.git and
// https://github.com/o/r.git are both https://github.com/o/r. It is ""
// for a remote it cannot tell, as a path on disk.
func WebURL(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	var host, path string
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		if u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh" && u.Scheme != "git" {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else if at := strings.Index(remote, "@"); at >= 0 && strings.Contains(remote[at:], ":") {
		// scp-like: git@host:owner/repo.git
		rest := remote[at+1:]
		i := strings.Index(rest, ":")
		host, path = rest[:i], rest[i+1:]
	} else {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return "https://" + host + "/" + path
}

// RemoteWebURL is the web page of the repository of remote name, or "".
func RemoteWebURL(ctx context.Context, root, name string) string {
	out, err := Command(ctx, root, "remote", "get-url", name)
	if err != nil {
		return ""
	}
	return WebURL(string(out))
}
