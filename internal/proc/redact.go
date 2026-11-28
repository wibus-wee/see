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
	pending []byte
	secrets []string
	hold    int
}

// NewTail makes a Tail holding at most max bytes, redacting secrets found
// in env from its output.
func NewTail(max int, env []string) *Tail {
	secrets := secretsInEnv(env)
	hold := 0
	for _, secret := range secrets {
		if len(secret) > hold {
			hold = len(secret)
		}
	}
	return &Tail{max: max, secrets: secrets, hold: hold}
}

// Write redacts p before it enters the window, then keeps the last max
// bytes. Redaction must precede the trim: a secret longer than the window
// can never match inside it afterwards, so trimming first would expose
// oversized secrets — PEM bundles, certificate chains — wholesale. The
// replacement is shorter than any secret, so redacting first can only
// shrink what the window holds. A secret split across writes is held back
// until it either completes or is disproven, bounded by the longest
// secret, since partial output at the end of a write cannot yet be told
// from the start of one.
func (t *Tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.redact(append(t.pending, p...), false)
	return len(p), nil
}

// redact absorbs data, replacing every secret that appears, and moves
// what no future byte can still complete into the window. final flushes
// the holdback: no more input can complete a partial secret there.
func (t *Tail) redact(data []byte, final bool) {
	if len(data) == 0 {
		return
	}
	s := string(data)
	for _, secret := range t.secrets {
		s = strings.ReplaceAll(s, secret, "<redacted>")
	}
	// A secret already redacted cannot complete again, and a partial
	// match can only live in the last len(secret)-1 bytes, so holding
	// back exactly that much disproves nothing redaction missed.
	emit := len(s)
	if !final {
		holdback := max(t.hold-1, 0)
		emit = max(len(s)-holdback, 0)
	}
	t.buf = append(t.buf, s[:emit]...)
	t.pending = append(t.pending[:0], s[emit:]...)
	if len(t.buf) > t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
}

// String returns the redacted tail as whole lines: a partial first line
// left by the bound is dropped.
func (t *Tail) String() string {
	t.mu.Lock()
	t.redact(t.pending, true)
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
	return strings.TrimRight(s, "\n")
}
