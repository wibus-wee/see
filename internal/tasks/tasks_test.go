package tasks

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
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
			conn, err := net.Dial("tcp", os.Getenv("SEE_TASK_CHILD_ADDRESS"))
			if err != nil {
				panic(err)
			}
			defer conn.Close()
			fmt.Fprintln(conn, os.Getpid())
			// Remain alive with the socket open until killed or the test cleans up.
			var b [1]byte
			_, _ = conn.Read(b[:])
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_TASK_HELPER", "tree")
	t.Setenv("SEE_TASK_CHILD_ADDRESS", listener.Addr().String())
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	root := t.TempDir()
	go func() { finished <- Run(ctx, executable, root, Test, &Output{}) }()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal("child never connected:", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal("child handshake:", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	// Clean up a survivor even when the production cancellation is broken.
	defer func() {
		if child, err := os.FindProcess(pid); err == nil {
			_ = child.Kill()
		}
	}()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("task did not finish after cancellation")
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err = reader.ReadByte()
	if err == nil {
		t.Fatal("unexpected child output")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("descendant remains alive after task cancellation")
	}
	if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		// A forcibly terminated process may reset its TCP connection.
		if operation, ok := err.(*net.OpError); !ok || operation.Op != "read" {
			t.Fatalf("child exit: %v", err)
		}
	}
}
