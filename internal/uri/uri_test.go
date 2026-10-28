package uri

import (
	"errors"
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
