package search

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("SEE_SEARCH_HELPER"); mode != "" {
		if mode == "long" {
			fmt.Println(strings.Repeat("x", 128<<10))
			os.Exit(0)
		}
		if mode == "empty" {
			os.Exit(1)
		}
		fmt.Println("partial.go")
		if mode == "fail" {
			fmt.Fprintln(os.Stderr, "workspace scan failed")
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A partial list from a failed scanner must not look like a complete workspace.
func TestFileListingReportsProcessFailureAndCanRetry(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv("SEE_SEARCH_HELPER", "fail")
	if files, truncated, err := ListFiles(context.Background(), executable, root, 100, nil); err == nil || truncated {
		t.Fatalf("failed process reported success: files=%q truncated=%v err=%v", files, truncated, err)
	} else if !strings.Contains(err.Error(), "workspace scan failed") {
		t.Fatalf("failure reason lost: %v", err)
	}
	t.Setenv("SEE_SEARCH_HELPER", "long")
	if _, _, err := ListFiles(context.Background(), executable, root, 100, nil); err == nil {
		t.Fatal("oversized listing record silently accepted")
	}
	t.Setenv("SEE_SEARCH_HELPER", "empty")
	if files, truncated, err := ListFiles(context.Background(), executable, root, 100, nil); err != nil || truncated || len(files) != 0 {
		t.Fatalf("no files: %q %v %v", files, truncated, err)
	}
	t.Setenv("SEE_SEARCH_HELPER", "ok")
	files, truncated, err := ListFiles(context.Background(), executable, root, 100, nil)
	if err != nil || truncated || !slices.Equal(files, []string{"partial.go"}) {
		t.Fatalf("retry: files=%q truncated=%v err=%v", files, truncated, err)
	}
}
