// Package uri converts between local file paths and file:// URIs, as the
// Language Server Protocol exchanges them. Paths are never spliced into
// URIs as strings: drive letters, spaces, non-ASCII names and percent
// escapes all go through net/url.
package uri

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrUnsupportedScheme is returned for URIs that do not name a local file,
// such as jar: or untitled:.
var ErrUnsupportedScheme = errors.New("unsupported URI scheme")

// FromPath returns the file:// URI of a path of the running OS; a
// relative path is taken from the current directory.
func FromPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return fromPath(path, runtime.GOOS == "windows")
}

// ToPath returns the local path of a file:// URI on the running OS.
func ToPath(u string) (string, error) { return toPath(u, runtime.GOOS == "windows") }

// fromPath expects an absolute path; one that is not is rooted, so the
// URI stays well formed (file:///x, never file:x or file:////x).
func fromPath(path string, windows bool) string {
	p := path
	if windows {
		p = strings.ReplaceAll(p, `\`, "/")
		if strings.HasPrefix(p, "//") {
			// A UNC path, \\server\share\x → file://server/share/x.
			host, tail, _ := strings.Cut(strings.TrimPrefix(p, "//"), "/")
			return (&url.URL{Scheme: "file", Host: host, Path: "/" + tail}).String()
		}
		if len(p) >= 2 && p[1] == ':' {
			// C:/x → /c:/x, the drive lower-cased as VS Code and gopls write it.
			p = strings.ToLower(p[:1]) + p[1:]
		}
	} else {
		p = filepath.ToSlash(p)
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func toPath(raw string, windows bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URI %q: %w", raw, err)
	}
	if !strings.EqualFold(u.Scheme, "file") {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedScheme, u.Scheme)
	}
	p := u.Path // already percent-decoded
	if windows {
		if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
			return `\\` + u.Host + strings.ReplaceAll(p, "/", `\`), nil
		}
		// /c:/x → C:\x
		if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
			p = strings.ToUpper(p[1:2]) + p[2:]
		}
		return strings.ReplaceAll(p, "/", `\`), nil
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", fmt.Errorf("%w: remote host %q", ErrUnsupportedScheme, u.Host)
	}
	if p == "" {
		return "", fmt.Errorf("invalid file URI %q: empty path", raw)
	}
	return p, nil
}
