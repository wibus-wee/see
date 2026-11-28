package uri

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixRoundTrip(t *testing.T) {
	for _, tc := range []struct{ path, uri string }{
		{"/home/u/src/main.go", "file:///home/u/src/main.go"},
		{"/home/u/my project/a b.go", "file:///home/u/my%20project/a%20b.go"},
		{"/home/u/源码/文件.go", "file:///home/u/%E6%BA%90%E7%A0%81/%E6%96%87%E4%BB%B6.go"},
		{"/tmp/100%/x#y?.go", "file:///tmp/100%25/x%23y%3F.go"},
	} {
		got := fromPath(tc.path, false)
		if got != tc.uri {
			t.Errorf("fromPath(%q) = %q, want %q", tc.path, got, tc.uri)
		}
		back, err := toPath(got, false)
		if err != nil || back != tc.path {
			t.Errorf("toPath(%q) = %q, %v; want %q", got, back, err, tc.path)
		}
	}
}

func TestWindowsRoundTrip(t *testing.T) {
	for _, tc := range []struct{ path, uri, back string }{
		{`C:\Users\u\src\main.go`, "file:///c:/Users/u/src/main.go", `C:\Users\u\src\main.go`},
		{`d:\My Code\ü.go`, "file:///d:/My%20Code/%C3%BC.go", `D:\My Code\ü.go`},
		{`\\server\share\a.go`, "file://server/share/a.go", `\\server\share\a.go`},
	} {
		got := fromPath(tc.path, true)
		if got != tc.uri {
			t.Errorf("fromPath(%q) = %q, want %q", tc.path, got, tc.uri)
		}
		back, err := toPath(got, true)
		if err != nil || back != tc.back {
			t.Errorf("toPath(%q) = %q, %v; want %q", got, back, err, tc.back)
		}
	}
}

func TestToPathDecodesServerURIs(t *testing.T) {
	// gopls escapes the drive colon on Windows; both forms must decode.
	for _, u := range []string{"file:///C:/x/y.go", "file:///c%3A/x/y.go"} {
		p, err := toPath(u, true)
		if err != nil || p != `C:\x\y.go` {
			t.Errorf("toPath(%q) = %q, %v", u, p, err)
		}
	}
	p, err := toPath("file://localhost/etc/hosts", false)
	if err != nil || p != "/etc/hosts" {
		t.Errorf("localhost: %q, %v", p, err)
	}
}

func TestUnsupportedScheme(t *testing.T) {
	for _, u := range []string{"jar:file:///x.jar!/A.class", "untitled:Untitled-1", "https://example.com/a.go", "file://otherhost/x"} {
		if _, err := toPath(u, false); !errors.Is(err, ErrUnsupportedScheme) {
			t.Errorf("toPath(%q) err = %v, want ErrUnsupportedScheme", u, err)
		}
	}
}

func TestRelativePathsStayWellFormed(t *testing.T) {
	if got := fromPath("x/y.go", false); got != "file:///x/y.go" {
		t.Errorf("unix relative: %q", got)
	}
	if got := fromPath(`x\y.go`, true); got != "file:///x/y.go" {
		t.Errorf("windows relative: %q", got)
	}
	if got := FromPath("rel.go"); !strings.HasPrefix(got, "file:///") || strings.HasPrefix(got, "file:////") {
		t.Errorf("FromPath relative: %q", got)
	}
}

// TestRealFileRoundTrip exercises the codec against files that exist:
// names with a percent sign, spaces, a question mark, a hash and
// non-ASCII bytes are written to disk, their URIs decode back to the
// one path the file answers, and decoding twice would have lost them.
func TestRealFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"100%.go",
		"a b.go",
		"what?.go",
		"hash#x.go",
		"文件 源码%.go",
		"%25.go",   // the name IS the encoding of '%'
		"%20.go",   // the name IS the encoding of a space
		"plus+&.go",
	}
	seen := map[string]bool{}
	created := 0
	for _, name := range names {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
			// A name the platform's file namespace cannot carry —
			// what?.go under Win32, where ? is a wildcard — is not a
			// test of the codec: the URI of such a file never exists
			// there. The remaining names still cover the escapes.
			t.Logf("skipping %q: unrepresentable here (%v)", name, err)
			continue
		}
		created++
		u := FromPath(p)
		if seen[u] {
			t.Errorf("%q and another name share URI %q", name, u)
		}
		seen[u] = true
		back, err := ToPath(u)
		if err != nil {
			t.Fatalf("ToPath(%q): %v", u, err)
		}
		if back != p {
			t.Errorf("round trip of %q: %q -> %q", name, u, back)
			continue
		}
		// The decoded path must be the file on disk, byte for byte.
		data, err := os.ReadFile(back)
		if err != nil || string(data) != name {
			t.Errorf("reading %q back: %q, %v", back, data, err)
		}
		// Decoding the URI a second time must not happen: %25 is the
		// name, and a codec that decodes twice would look for '%'.
		if strings.Contains(back, "%") != strings.Contains(p, "%") {
			t.Errorf("double decode of %q: %q", u, back)
		}
	}
	if len(seen) != created {
		t.Errorf("URI collision: %d URIs for %d names", len(seen), created)
	}
}

// TestToPathDoesNotDecodeTwice pins the codec against double decoding:
// '%25' in a URI is the percent sign of the name, never the start of
// another escape.
func TestToPathDoesNotDecodeTwice(t *testing.T) {
	p, err := ToPath("file:///tmp/100%25/x%25y.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "100%"+string(filepath.Separator)+"x%y.go") && p != filepath.FromSlash("/tmp/100%/x%y.go") {
		t.Errorf("got %q, want the percent signs literal", p)
	}
}

func TestOpaqueAndMalformedURIsRejected(t *testing.T) {
	for _, u := range []string{
		"file:///tmp/%zz.go",     // invalid escape
		"file:relative.go",       // opaque, not a path
		"file:///tmp/%",          // truncated escape
		"not a uri at all %",
	} {
		if _, err := ToPath(u); err == nil {
			t.Errorf("ToPath(%q) succeeded", u)
		}
	}
}
