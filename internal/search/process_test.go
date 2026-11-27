package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
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
		if mode == "slow" {
			// A scanner that takes much longer than a test is willing
			// to wait: cancellation, not patience, ends the scan.
			for i := range 100000 {
				fmt.Printf("file%05d.go\n", i)
				time.Sleep(10 * time.Millisecond)
			}
			os.Exit(0)
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
	if files, truncated, err := ListFiles(context.Background(), executable, root, 100, nil, nil, IgnoreFileOptions{}); err == nil || truncated {
		t.Fatalf("failed process reported success: files=%q truncated=%v err=%v", files, truncated, err)
	} else if !strings.Contains(err.Error(), "workspace scan failed") {
		t.Fatalf("failure reason lost: %v", err)
	}
	t.Setenv("SEE_SEARCH_HELPER", "long")
	if _, _, err := ListFiles(context.Background(), executable, root, 100, nil, nil, IgnoreFileOptions{}); err == nil {
		t.Fatal("oversized listing record silently accepted")
	}
	t.Setenv("SEE_SEARCH_HELPER", "empty")
	if files, truncated, err := ListFiles(context.Background(), executable, root, 100, nil, nil, IgnoreFileOptions{}); err != nil || truncated || len(files) != 0 {
		t.Fatalf("no files: %q %v %v", files, truncated, err)
	}
	t.Setenv("SEE_SEARCH_HELPER", "ok")
	files, truncated, err := ListFiles(context.Background(), executable, root, 100, nil, nil, IgnoreFileOptions{})
	if err != nil || truncated || !slices.Equal(files, []string{"partial.go"}) {
		t.Fatalf("retry: files=%q truncated=%v err=%v", files, truncated, err)
	}
}

// A canceled scan stops instead of running the scanner to completion, and
// reports the cancellation rather than the truncated output it happened
// to collect: partial output is not an empty workspace (REQ-LIST-030).
func TestFileListingCancellationStopsTheScanner(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_SEARCH_HELPER", "slow")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	files, truncated, err := ListFiles(ctx, executable, t.TempDir(), 200000, nil, nil, IgnoreFileOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if files != nil || truncated {
		t.Fatalf("canceled scan returned files=%v truncated=%v", files, truncated)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("scan took %v after cancellation; the scanner outlived its context", elapsed)
	}
}

// The walk path honors the same cancellation contract as the scanner, and
// a limit caps what a scan can hold however large the tree.
func TestFileWalkCancellationAndLimit(t *testing.T) {
	root := tree(t, map[string]string{
		"a/1.go": "", "a/2.go": "", "b/3.go": "",
	})
	ctx, cancel := context.WithCancel(context.Background())
	files, truncated, err := ListFiles(ctx, "", root, 100, func(rel string) bool {
		cancel() // cancel as soon as the walk visits anything
		return false
	}, nil, IgnoreFileOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("walk err = %v, want context.Canceled", err)
	}
	if files != nil || truncated {
		t.Fatalf("canceled walk returned files=%v truncated=%v", files, truncated)
	}
	files, truncated, err = ListFiles(context.Background(), "", root, 2, nil, nil, IgnoreFileOptions{})
	if err != nil || !truncated || len(files) != 2 {
		t.Fatalf("limited walk: files=%q truncated=%v err=%v", files, truncated, err)
	}
}
