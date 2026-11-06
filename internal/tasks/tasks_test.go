package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("SEE_TASK_HELPER"); mode != "" {
		dir, _ := os.Getwd()
		fmt.Printf("args=%s dir=%s\n", strings.Join(os.Args[1:], " "), dir)
		fmt.Fprintln(os.Stderr, "stderr captured")
		switch mode {
		case "fail":
			os.Exit(7)
		case "wait":
			time.Sleep(30 * time.Second)
		case "child":
			time.Sleep(350 * time.Millisecond)
			_ = os.WriteFile(os.Getenv("SEE_TASK_MARKER"), []byte("survived"), 0600)
		case "tree":
			child := exec.Command(os.Args[0])
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "SEE_TASK_HELPER=") {
					child.Env = append(child.Env, entry)
				}
			}
			child.Env = append(child.Env, "SEE_TASK_HELPER=child")
			if err := child.Start(); err != nil {
				panic(err)
			}
			fmt.Println("child started")
			time.Sleep(30 * time.Second)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRunFixedArgumentsAndWorkingDirectory(t *testing.T) {
	t.Setenv("SEE_TASK_HELPER", "ok")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, kind := range []Kind{Build, Test} {
		output := &Output{}
		if err := Run(context.Background(), executable, root, kind, output); err != nil {
			t.Fatal(err)
		}
		text, _ := output.Snapshot()
		if !strings.Contains(text, "args="+string(kind)+" ./... dir="+root) || !strings.Contains(text, "stderr captured") {
			t.Fatalf("output: %q", text)
		}
	}
	if err := Run(context.Background(), executable, root, Kind("custom"), &Output{}); err == nil {
		t.Fatal("accepted unsupported task")
	}
}

func TestRunCancellationAndFailure(t *testing.T) {
	executable, _ := os.Executable()
	t.Setenv("SEE_TASK_HELPER", "fail")
	if err := Run(context.Background(), executable, t.TempDir(), Build, &Output{}); err == nil {
		t.Fatal("exit error lost")
	}
	t.Setenv("SEE_TASK_HELPER", "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := Run(ctx, executable, t.TempDir(), Test, &Output{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestOutputBoundedDuringConcurrentWrites(t *testing.T) {
	output := &Output{}
	var writers sync.WaitGroup
	for range 4 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 50 {
				output.Write([]byte(strings.Repeat("a", 8192)))
				output.Snapshot()
			}
		}()
	}
	writers.Wait()
	text, truncated := output.Snapshot()
	if len(text) > MaxOutput || !truncated {
		t.Fatalf("len=%d truncated=%v", len(text), truncated)
	}
	output.Write([]byte(strings.Repeat("b", MaxOutput*2)))
	text, _ = output.Snapshot()
	if text != strings.Repeat("b", MaxOutput) {
		t.Fatal("newest output not retained")
	}
}

func TestRunCancelsDescendants(t *testing.T) {
	t.Setenv("SEE_TASK_HELPER", "tree")
	marker := filepath.Join(t.TempDir(), "child-survived")
	t.Setenv("SEE_TASK_MARKER", marker)
	executable, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output := &Output{}
	output.Changed = func() {
		text, _ := output.Snapshot()
		if strings.Contains(text, "child started") {
			cancel()
		}
	}
	if err := Run(ctx, executable, t.TempDir(), Test, output); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("descendant survived task cancellation: %v", err)
	}
}
