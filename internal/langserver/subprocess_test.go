package langserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/operr"
)

func TestLifecyclePeer(t *testing.T) {
	if os.Getenv("SEE_LIFECYCLE_PEER") != "1" {
		return
	}
	var text string
	var version int
	var pending int
	r := bufio.NewReader(os.Stdin)
	for {
		body, err := lsp.ReadMessage(r)
		if err != nil {
			os.Exit(2)
		}
		var m struct {
			ID     int
			Method string
			Params struct {
				ID           int
				TextDocument struct {
					Text    string
					Version int
				}
				ContentChanges []struct{ Text string }
			}
		}
		if json.Unmarshal(body, &m) != nil {
			os.Exit(3)
		}
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{"capabilities": map[string]any{"hoverProvider": true, "definitionProvider": true, "textDocumentSync": 1}}
		case "textDocument/didOpen":
			text, version = m.Params.TextDocument.Text, m.Params.TextDocument.Version
			if os.WriteFile("opened", []byte(m.Params.TextDocument.Text), 0600) != nil {
				os.Exit(4)
			}
			continue
		case "textDocument/didChange":
			if m.Params.TextDocument.Version <= version || len(m.Params.ContentChanges) != 1 {
				os.Exit(8)
			}
			version, text = m.Params.TextDocument.Version, m.Params.ContentChanges[0].Text
			continue
		case "textDocument/hover":
			if pending == 0 {
				pending = m.ID
				if os.WriteFile("pending", nil, 0600) != nil {
					os.Exit(9)
				}
				continue
			}
			result = map[string]string{"contents": text}
		case "$/cancelRequest":
			if pending != m.Params.ID {
				os.Exit(10)
			}
			m.ID = pending
			result = map[string]string{"contents": "obsolete"}
		case "textDocument/definition":
			os.Exit(5)
		case "shutdown":
		case "exit":
			os.Exit(0)
		default:
			continue
		}
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
		if lsp.WriteMessage(os.Stdout, b) != nil {
			os.Exit(6)
		}
		if m.Method == "$/cancelRequest" {
			if os.WriteFile("late-reply", nil, 0600) != nil {
				os.Exit(11)
			}
		}
	}
}

func TestCrashReplaysCurrentDocumentAndExhaustsBudget(t *testing.T) {
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := restartDelays
	defer func() { restartDelays = old }()
	restartDelays = [maxRestarts]time.Duration{time.Millisecond * 20, time.Millisecond * 20}
	s := newProcessServer(root, exe)
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const uri = "file:///a.go"
	buf := editor.NewBuffer("original")
	if err = s.Sync(ctx, c, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	buf.Set("latest")
	if err = s.Sync(ctx, c, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxRestarts; i++ {
		_, err = c.Definition(ctx, uri, lsp.Position{})
		if err == nil || ctx.Err() != nil {
			t.Fatalf("crash result %v, context %v", err, ctx.Err())
		}
		if i == maxRestarts {
			break
		}
		prev := c
		waitFor(t, func() bool { c = s.Current(); return c != nil && c != prev })
		waitFor(t, func() bool { b, _ := os.ReadFile(root + "/opened"); return string(b) == "latest" })
		if err = os.Remove(root + "/opened"); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { return serverState(s) == Failed })
	time.Sleep(3 * restartDelays[0])
	state, cause := s.Status()
	var failure *operr.Error
	if state != Failed || !errors.As(cause, &failure) || failure.Status != operr.Persistent || failure.Attempt != maxRestarts+1 {
		t.Fatalf("budget exhaustion: %v %+v", state, cause)
	}
}

func newProcessServer(root, exe string) *Server {
	return New(Options{Root: root, Start: func(ctx context.Context, root string, _ []lsp.WorkspaceFolder, exit func(error)) (Client, error) {
		return lsp.Start(ctx, exe, []string{"-test.run=^TestLifecyclePeer$"}, lsp.Options{RootPath: root, Env: []string{"SEE_LIFECYCLE_PEER=1"}, OnExit: exit})
	}})
}

func TestLateReplyAfterRapidChangesAndCancellation(t *testing.T) {
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s := newProcessServer(root, exe)
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const uri = "file:///edited.go"
	buf := editor.NewBuffer("initial")
	if err = s.Sync(ctx, c, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	old, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { _, _, err := c.Hover(old, uri, lsp.Position{}); done <- err }()
	waitFor(t, func() bool { _, err := os.Stat(root + "/pending"); return err == nil })
	for i := range 12 {
		buf.Set(fmt.Sprint("edit", i))
		s.Changed(uri, buf.Snapshot())
	}
	if err = s.Sync(ctx, c, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	stop()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("obsolete request hung")
	}
	waitFor(t, func() bool { _, err := os.Stat(root + "/late-reply"); return err == nil })
	got, _, err := c.Hover(ctx, uri, lsp.Position{})
	if err != nil || got != "edit11" {
		t.Fatalf("latest hover %q %v", got, err)
	}
}

func TestStableSessionEarnsRestartBudgetBack(t *testing.T) {
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	old := restartDelays
	defer func() { restartDelays = old }()
	restartDelays = [maxRestarts]time.Duration{time.Millisecond, time.Millisecond}
	s := newProcessServer(root, exe)
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= maxRestarts; i++ {
		if i == maxRestarts {
			s.mu.Lock()
			s.readySince = s.readySince.Add(-stableUptime)
			s.mu.Unlock()
		}
		if _, err := c.Definition(ctx, "file:///a.go", lsp.Position{}); err == nil || ctx.Err() != nil {
			t.Fatalf("crash result %v", err)
		}
		old := c
		waitFor(t, func() bool { c = s.Current(); return c != nil && c != old })
	}
}
