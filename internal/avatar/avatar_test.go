package avatar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestURLs(t *testing.T) {
	if got := URLs("123+octo@users.noreply.github.com", false, 64); len(got) != 1 || got[0] != "https://avatars.githubusercontent.com/u/123?s=64" {
		t.Errorf("noreply with id: %q", got)
	}
	if got := URLs("octo@users.noreply.github.com", false, 64); len(got) != 1 || got[0] != "https://github.com/octo.png?size=64" {
		t.Errorf("noreply: %q", got)
	}
	got := URLs(" A@B.com ", true, 64)
	if len(got) != 2 || !strings.Contains(got[0], "email=a%40b.com") || !strings.HasPrefix(got[1], "https://www.gravatar.com/avatar/") {
		t.Errorf("on GitHub: %q", got)
	}
	if got := URLs("a@b.com", false, 64); len(got) != 1 || !strings.Contains(got[0], "gravatar") {
		t.Errorf("elsewhere: %q", got)
	}
}

func TestFetchFallsBackAndCaches(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/none" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	urls := []string{srv.URL + "/none", srv.URL + "/pic"}
	for range 2 {
		data, err := Fetch(context.Background(), srv.Client(), dir, urls)
		if err != nil || string(data) != "png" {
			t.Fatalf("%q %v", data, err)
		}
	}
	if hits != 2 {
		t.Fatalf("%d requests, want 2: the second fetch comes from the cache", hits)
	}
	if _, err := Fetch(context.Background(), srv.Client(), dir, urls[:1]); err != ErrNone {
		t.Fatalf("none: %v", err)
	}
	if _, err := Fetch(context.Background(), srv.Client(), dir, urls[:1]); err != ErrNone || hits != 3 {
		t.Fatalf("none again: %v after %d requests", err, hits)
	}
}
