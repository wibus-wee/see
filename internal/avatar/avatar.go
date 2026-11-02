// Package avatar finds the picture of a commit's author by their email,
// as GitHub and Xcode show it: their GitHub avatar, else their Gravatar,
// kept on disk for a week so that a history does not ask again.
package avatar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Person is an author or co-author of a commit.
type Person struct{ Name, Email string }

var noreply = regexp.MustCompile(`^(?:(\d+)\+)?([^@]+)@users\.noreply\.github\.com$`)

// URLs are where to look for the picture of email, in order: GitHub's
// for one of its noreply addresses, or for any address of a repository
// on GitHub, then Gravatar's, which answers 404 for an address it does
// not know.
func URLs(email string, onGitHub bool, size int) []string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}
	s := url.QueryEscape(itoa(size))
	if m := noreply.FindStringSubmatch(email); m != nil {
		if m[1] != "" {
			return []string{"https://avatars.githubusercontent.com/u/" + m[1] + "?s=" + s}
		}
		return []string{"https://github.com/" + url.PathEscape(m[2]) + ".png?size=" + s}
	}
	var urls []string
	if onGitHub {
		urls = append(urls, "https://avatars.githubusercontent.com/u/e?email="+url.QueryEscape(email)+"&s="+s)
	}
	sum := sha256.Sum256([]byte(email))
	return append(urls, "https://www.gravatar.com/avatar/"+hex.EncodeToString(sum[:])+"?d=404&s="+s)
}

func itoa(n int) string {
	if n <= 0 {
		return "64"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// ErrNone is Fetch's answer for an address with no picture anywhere.
var ErrNone = errors.New("no avatar")

// cacheTTL is how long a picture, or the knowledge that there is none,
// is kept.
const cacheTTL = 7 * 24 * time.Hour

// Fetch returns the picture at the first of urls that has one, from the
// cache in dir ("" for none) while it is fresh.
func Fetch(ctx context.Context, client *http.Client, dir string, urls []string) ([]byte, error) {
	if len(urls) == 0 {
		return nil, ErrNone
	}
	sum := sha256.Sum256([]byte(strings.Join(urls, "\n")))
	file := ""
	if dir != "" {
		file = filepath.Join(dir, hex.EncodeToString(sum[:16]))
		if fi, err := os.Stat(file); err == nil && time.Since(fi.ModTime()) < cacheTTL {
			data, err := os.ReadFile(file)
			if err == nil && len(data) == 0 {
				return nil, ErrNone
			}
			if err == nil {
				return data, nil
			}
		}
	}
	data, err := download(ctx, client, urls)
	if err != nil && !errors.Is(err, ErrNone) {
		return nil, err // offline, say: ask again next time
	}
	if file != "" && os.MkdirAll(dir, 0o755) == nil {
		_ = os.WriteFile(file, data, 0o644) // empty for none
	}
	return data, err
}

func download(ctx context.Context, client *http.Client, urls []string) ([]byte, error) {
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusOK && strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
			return data, nil
		}
		if resp.StatusCode != http.StatusNotFound {
			return nil, errors.New(resp.Status)
		}
	}
	return nil, ErrNone
}

// CacheDir is where pictures are kept, or "".
func CacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "See", "avatars")
}

var trailer = regexp.MustCompile(`(?im)^co-authored-by:\s*(.+?)\s*<([^>]+)>\s*$`)

// CoAuthors are the people a commit message's Co-authored-by trailers
// name, each once.
func CoAuthors(message string) []Person {
	var out []Person
	seen := map[string]bool{}
	for _, m := range trailer.FindAllStringSubmatch(message, -1) {
		email := strings.ToLower(m[2])
		if !seen[email] {
			seen[email] = true
			out = append(out, Person{Name: m[1], Email: m[2]})
		}
	}
	return out
}
