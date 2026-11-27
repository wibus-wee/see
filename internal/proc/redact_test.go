package proc

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	env := []string{
		"HOME=/Users/someone",
		"GITHUB_TOKEN=ghp_0123456789abcdef",
		"GOPROXY=https://user:passw0rd123456@proxy.example.com",
		"PATH=/usr/bin:/bin",
		"EDITOR=vim",
	}
	out := RedactSecrets("token ghp_0123456789abcdef and proxy passw0rd123456 and path /Users/someone", env)
	if strings.Contains(out, "ghp_0123456789abcdef") || strings.Contains(out, "passw0rd123456") {
		t.Fatalf("secret survived redaction: %q", out)
	}
	if !strings.Contains(out, "/Users/someone") {
		t.Fatalf("harmless path was redacted: %q", out)
	}
}

func TestRedactSecretsSkipsShortValues(t *testing.T) {
	// Values too short to be credentials stay visible: redacting them
	// would hide ordinary output like version strings.
	env := []string{"API_KEY=abc", "VERBOSE=very-verbose-mode-enabled"}
	out := RedactSecrets("key abc mode very-verbose-mode-enabled", env)
	if !strings.Contains(out, "abc") {
		t.Fatalf("short value redacted: %q", out)
	}
	if strings.Contains(out, "very-verbose-mode-enabled") {
		t.Fatalf("long single-token value not redacted: %q", out)
	}
}

func TestTailKeepsLastBytesAsLines(t *testing.T) {
	tail := NewTail(32, nil)
	for range 8 {
		if _, err := tail.Write([]byte("some log line here\n")); err != nil {
			t.Fatal(err)
		}
	}
	got := tail.String()
	if len(got) > 32 {
		t.Fatalf("tail exceeded its bound: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "some log line here") {
		t.Fatalf("tail lost the most recent line: %q", got)
	}
	if strings.Contains(got, "\n\n") {
		t.Fatalf("tail mangled lines: %q", got)
	}
	if empty := NewTail(32, nil); empty.String() != "" {
		t.Fatal("empty tail not empty")
	}
}

func TestTailRedactsEnvironmentSecrets(t *testing.T) {
	env := []string{"PRIVATE_TOKEN=0123456789abcdef"}
	tail := NewTail(1024, env)
	if _, err := tail.Write([]byte("fatal: auth 0123456789abcdef failed\n")); err != nil {
		t.Fatal(err)
	}
	got := tail.String()
	if strings.Contains(got, "0123456789abcdef") {
		t.Fatalf("secret survived in tail: %q", got)
	}
	if !strings.Contains(got, "<redacted>") {
		t.Fatalf("redaction marker missing: %q", got)
	}
}
