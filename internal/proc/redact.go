package proc

import (
	"slices"
	"strings"
	"sync"
)

// secretNameParts are the lowercase substrings of environment variable
// names whose values are treated as secrets: tokens, passwords, keys.
var secretNameParts = []string{
	"token", "secret", "password", "passwd", "credential",
	"auth", "apikey", "api_key", "private",
}

// secretEnvValue reports whether an environment value is sensitive enough
// to redact from captured process output. Values of secret-named variables
// are always sensitive; other values count when they look like credentials:
// long single tokens with no spaces, such as keys embedded in GOPROXY URLs.
func secretEnvValue(name, value string) bool {
	if len(value) < 8 {
		return false // too short to be a credential, redacting would only hide paths
	}
	lower := strings.ToLower(name)
	for _, part := range secretNameParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return len(value) >= 20 && !strings.ContainsAny(value, " \t\n")
}

// secretsInEnv collects the exact strings to redact: sensitive environment
// values plus passwords embedded in URL userinfo, which a process can echo
// without the rest of the value. Sorted longest first so that a secret
// nested inside another is never left half-visible by replacement order.
func secretsInEnv(env []string) []string {
	var secrets []string
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if secretEnvValue(name, value) {
			secrets = append(secrets, value)
		}
		if pass := urlPassword(value); len(pass) >= 6 {
			secrets = append(secrets, pass)
		}
	}
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	return secrets
}

// urlPassword extracts the password of the first URL userinfo in value:
// the "pass" of scheme://user:pass@host.
func urlPassword(value string) string {
	i := strings.Index(value, "://")
	if i < 0 {
		return ""
	}
	authority := value[i+3:]
	if j := strings.IndexAny(authority, "/?#"); j >= 0 {
		authority = authority[:j]
	}
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return ""
	}
	userinfo := authority[:at]
	colon := strings.Index(userinfo, ":")
	if colon < 0 {
		return ""
	}
	return userinfo[colon+1:]
}

// RedactSecrets replaces occurrences of sensitive environment values in s
// with "<redacted>". env entries use the "NAME=value" format. Values too
// short to be credentials are left alone so paths and versions stay legible.
func RedactSecrets(s string, env []string) string {
	for _, secret := range secretsInEnv(env) {
		s = strings.ReplaceAll(s, secret, "<redacted>")
	}
	return s
}

// Tail is a writer that keeps only the last max bytes written to it,
// redacting secrets found in the given environment from its output. It is
// safe for concurrent use, though a process's stderr copies through a
// single goroutine.
type Tail struct {
	mu      sync.Mutex
	max     int
	buf     []byte
	secrets []string
}

// NewTail makes a Tail holding at most max bytes, redacting secrets found
// in env from its output.
func NewTail(max int, env []string) *Tail {
	return &Tail{max: max, secrets: secretsInEnv(env)}
}

func (t *Tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

// String returns the redacted tail as whole lines: a partial first line
// left by the bound is dropped.
func (t *Tail) String() string {
	t.mu.Lock()
	s := string(t.buf)
	full := len(t.buf) == t.max
	t.mu.Unlock()
	if len(s) == 0 {
		return ""
	}
	if full {
		if i := strings.IndexByte(s, '\n'); i >= 0 && i+1 < len(s) {
			s = s[i+1:]
		}
	}
	s = strings.TrimRight(s, "\n")
	for _, secret := range t.secrets {
		s = strings.ReplaceAll(s, secret, "<redacted>")
	}
	return s
}
