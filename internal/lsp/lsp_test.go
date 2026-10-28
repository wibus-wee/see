package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestColumns(t *testing.T) {
	// a=1 byte, 中=3 bytes/1 UTF-16 unit, 🎉=4 bytes/2 units, é as e+U+0301
	// (a combining mark, its own code point).
	line := "a中🎉éx\r"
	type pt struct{ bytes, u16, u8, u32 int }
	for _, p := range []pt{
		{0, 0, 0, 0},
		{1, 1, 1, 1},   // after a
		{4, 2, 4, 2},   // after 中
		{8, 4, 8, 3},   // after 🎉
		{9, 5, 9, 4},   // after e
		{11, 6, 11, 5}, // after the combining mark
		{12, 7, 12, 6}, // after x
		{13, 8, 13, 7}, // after \r, kept by a CRLF line read raw
	} {
		if got := ColumnToLSP(line, p.bytes, UTF16); got != p.u16 {
			t.Errorf("ToLSP(%d, utf-16) = %d, want %d", p.bytes, got, p.u16)
		}
		if got := ColumnToLSP(line, p.bytes, UTF8); got != p.u8 {
			t.Errorf("ToLSP(%d, utf-8) = %d, want %d", p.bytes, got, p.u8)
		}
		if got := ColumnToLSP(line, p.bytes, UTF32); got != p.u32 {
			t.Errorf("ToLSP(%d, utf-32) = %d, want %d", p.bytes, got, p.u32)
		}
		if got := ColumnFromLSP(line, p.u16, UTF16); got != p.bytes {
			t.Errorf("FromLSP(%d, utf-16) = %d, want %d", p.u16, got, p.bytes)
		}
		if got := ColumnFromLSP(line, p.u8, UTF8); got != p.bytes {
			t.Errorf("FromLSP(%d, utf-8) = %d, want %d", p.u8, got, p.bytes)
		}
		if got := ColumnFromLSP(line, p.u32, UTF32); got != p.bytes {
			t.Errorf("FromLSP(%d, utf-32) = %d, want %d", p.u32, got, p.bytes)
		}
	}
	// Inside the surrogate pair of 🎉: its start.
	if got := ColumnFromLSP(line, 3, UTF16); got != 4 {
		t.Errorf("mid-surrogate = %d, want 4", got)
	}
	// Inside 中's bytes: counts from its start.
	if got := ColumnToLSP(line, 2, UTF16); got != 1 {
		t.Errorf("mid-rune = %d, want 1", got)
	}
	if got := ColumnFromLSP(line, 6, UTF8); got != 4 {
		t.Errorf("utf-8 mid-rune = %d, want 4", got)
	}
	// Past the end clamps.
	if got := ColumnFromLSP("ab", 99, UTF16); got != 2 {
		t.Errorf("past end = %d", got)
	}
	if got := ColumnToLSP("ab", 99, UTF16); got != 2 {
		t.Errorf("past end = %d", got)
	}
	// Invalid UTF-8 counts a byte per unit and does not panic.
	if got := ColumnToLSP("a\xffb", 3, UTF16); got != 3 {
		t.Errorf("invalid = %d", got)
	}
	if NegotiatedEncoding("") != UTF16 || NegotiatedEncoding("utf-8") != UTF8 || NegotiatedEncoding("ucs-2") != UTF16 {
		t.Error("NegotiatedEncoding")
	}
}

func TestParseLocations(t *testing.T) {
	loc := `{"uri":"file:///a.go","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":5}}}`
	link := `{"targetUri":"file:///b.go","targetRange":{"start":{"line":0,"character":0},"end":{"line":9,"character":1}},"targetSelectionRange":{"start":{"line":3,"character":5},"end":{"line":3,"character":8}}}`
	for _, tc := range []struct {
		raw   string
		want  []Location
		isErr bool
	}{
		{"null", nil, false},
		{"", nil, false},
		{"[]", []Location{}, false},
		{loc, []Location{{URI: "file:///a.go", Range: Range{Position{1, 2}, Position{1, 5}}}}, false},
		{"[" + loc + "," + loc + "]", []Location{{"file:///a.go", Range{Position{1, 2}, Position{1, 5}}}, {"file:///a.go", Range{Position{1, 2}, Position{1, 5}}}}, false},
		{"[" + link + "]", []Location{{"file:///b.go", Range{Position{3, 5}, Position{3, 8}}}}, false},
		{`"nope"`, nil, true},
		{`[{"foo":1}]`, nil, true},
		{`{"uri":`, nil, true},
	} {
		got, err := ParseLocations(json.RawMessage(tc.raw))
		if (err != nil) != tc.isErr {
			t.Errorf("%s: err = %v", tc.raw, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v", tc.raw, got)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: [%d] = %v, want %v", tc.raw, i, got[i], tc.want[i])
			}
		}
	}
}

func TestReadMessage(t *testing.T) {
	in := "Content-Length: 2\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n{}" +
		"Content-Length: 7\r\n\r\n{\"a\":1}"
	r := bufio.NewReader(strings.NewReader(in))
	for _, want := range []string{"{}", `{"a":1}`} {
		got, err := ReadMessage(r)
		if err != nil || string(got) != want {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	if _, err := ReadMessage(r); err != io.EOF {
		t.Errorf("end: %v", err)
	}
	for _, bad := range []string{"Content-Length: x\r\n\r\n", "garbage\r\n\r\n", "Content-Length: 99\r\n\r\n{}"} {
		if _, err := ReadMessage(bufio.NewReader(strings.NewReader(bad))); err == nil || err == io.EOF {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

// fakeServer is an in-process language server that answers from a
// script, for testing the client without gopls.
type fakeServer struct {
	t   *testing.T
	in  *bufio.Reader
	out io.Writer
	mu  sync.Mutex
	got []string // methods received, in order

	definition func(params json.RawMessage) string // raw result
	caps       string
	replies    chan json.RawMessage // results of our requests to the client
}

func newFake(t *testing.T) (*fakeServer, io.Reader, io.WriteCloser) {
	cr, sw := io.Pipe() // server → client
	sr, cw := io.Pipe() // client → server
	f := &fakeServer{t: t, in: bufio.NewReader(sr), out: sw, replies: make(chan json.RawMessage, 8),
		caps: `{"positionEncoding":"utf-8","textDocumentSync":{"openClose":true,"change":2},"definitionProvider":true}`}
	t.Cleanup(func() { sw.Close(); sr.Close() })
	return f, cr, cw
}

func (f *fakeServer) send(v string) {
	if err := WriteMessage(f.out, []byte(v)); err != nil {
		f.t.Log("fake send:", err)
	}
}

func (f *fakeServer) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

func (f *fakeServer) serve() {
	for {
		body, err := ReadMessage(f.in)
		if err != nil {
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		json.Unmarshal(body, &m)
		if m.Method == "" {
			if m.Error != nil {
				f.replies <- m.Error
			} else {
				f.replies <- m.Result
			}
			continue
		}
		f.mu.Lock()
		f.got = append(f.got, m.Method)
		f.mu.Unlock()
		reply := func(result string) {
			f.send(`{"jsonrpc":"2.0","id":` + string(m.ID) + `,"result":` + result + `}`)
		}
		switch m.Method {
		case "initialize":
			// Interleave garbage, a notification and a request of the
			// server's before answering: none may stall the client.
			f.send(`{not json`)
			f.send(`{"jsonrpc":"2.0","method":"window/logMessage","params":{"type":3,"message":"hi"}}`)
			f.send(`{"jsonrpc":"2.0","id":"s1","method":"workspace/configuration","params":{"items":[{},{}]}}`)
			f.send(`{"jsonrpc":"2.0","id":"s2","method":"custom/unknown","params":{}}`)
			reply(`{"capabilities":` + f.caps + `}`)
		case "textDocument/definition":
			if f.definition == nil {
				reply("null")
			} else if r := f.definition(m.Params); r != "" {
				reply(r)
			}
		case "shutdown":
			reply("null")
		case "exit":
			f.out.(io.Closer).Close() // as a process's stdout closes
			return
		}
	}
}

func connect(t *testing.T, f *fakeServer, r io.Reader, w io.WriteCloser, onExit func(error)) *Client {
	go f.serve()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := Connect(ctx, r, w, Options{RootPath: "/w", RootURI: "file:///w", Name: "w", Logf: t.Logf, OnExit: onExit})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing of the connection may outlive the test, as a reply logging
	// after it ended would.
	t.Cleanup(func() {
		c.Kill()
		r.(io.Closer).Close()
		<-c.Exited()
	})
	// The server's two requests got answers: nulls, and method not found.
	for range 2 {
		got := string(<-f.replies)
		if got != "[null,null]" && !strings.Contains(got, "-32601") {
			t.Errorf("reply to server request: %s", got)
		}
	}
	return c
}

func TestClientLifecycle(t *testing.T) {
	f, r, w := newFake(t)
	c := connect(t, f, r, w, nil)
	if c.Encoding() != UTF8 || !c.HasDefinition() {
		t.Errorf("encoding %q, definition %v", c.Encoding(), c.HasDefinition())
	}
	c.DidOpen("file:///w/a.go", "go", 1, "package a\n")
	c.DidChange("file:///w/a.go", 2, "package a // b\n")

	calls := 0
	f.definition = func(p json.RawMessage) string {
		calls++
		switch calls {
		case 1:
			return "null"
		case 2:
			return `{"uri":"file:///w/b.go","range":{"start":{"line":4,"character":5},"end":{"line":4,"character":6}}}`
		default:
			return `[{"targetUri":"file:///w/c.go","targetRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"targetSelectionRange":{"start":{"line":7,"character":1},"end":{"line":7,"character":2}}},{"uri":"file:///w/d.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}]`
		}
	}
	ctx := context.Background()
	locs, err := c.Definition(ctx, "file:///w/a.go", Position{0, 8})
	if err != nil || len(locs) != 0 {
		t.Errorf("null: %v %v", locs, err)
	}
	locs, err = c.Definition(ctx, "file:///w/a.go", Position{0, 8})
	if err != nil || len(locs) != 1 || locs[0].URI != "file:///w/b.go" {
		t.Errorf("one: %v %v", locs, err)
	}
	locs, err = c.Definition(ctx, "file:///w/a.go", Position{0, 8})
	if err != nil || len(locs) != 2 || locs[0].Range.Start.Line != 7 {
		t.Errorf("links: %v %v", locs, err)
	}
	c.DidClose("file:///w/a.go")
	c.Shutdown(time.Minute)
	select {
	case <-c.Exited():
	default:
		t.Error("not exited after shutdown")
	}
	want := []string{"initialize", "initialized", "textDocument/didOpen", "textDocument/didChange",
		"textDocument/definition", "textDocument/definition", "textDocument/definition", "textDocument/didClose", "shutdown", "exit"}
	got := f.methods()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("methods:\n got %v\nwant %v", got, want)
	}
}

func TestCanceledCall(t *testing.T) {
	f, r, w := newFake(t)
	arrived := make(chan struct{}, 1)
	f.definition = func(json.RawMessage) string { arrived <- struct{}{}; return "" } // never answers
	c := connect(t, f, r, w, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := c.Definition(ctx, "file:///w/a.go", Position{})
		done <- err
	}()
	<-arrived
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	// $/cancelRequest went out, and the connection still works.
	f.definition = nil
	if _, err := c.Definition(context.Background(), "file:///w/a.go", Position{}); err != nil {
		t.Errorf("after cancel: %v", err)
	}
	if got := f.methods(); !strings.Contains(strings.Join(got, ","), "$/cancelRequest") {
		t.Errorf("no cancel sent: %v", got)
	}
}

func TestServerCrash(t *testing.T) {
	f, r, w := newFake(t)
	exited := make(chan error, 1)
	f.definition = func(json.RawMessage) string {
		f.out.(*io.PipeWriter).Close() // the server dies mid-request
		return ""
	}
	c := connect(t, f, r, w, func(err error) { exited <- err })
	if _, err := c.Definition(context.Background(), "file:///w/a.go", Position{}); err == nil {
		t.Error("call to a dead server succeeded")
	}
	if err := <-exited; err == nil {
		t.Error("OnExit without a reason")
	}
	if _, err := c.Definition(context.Background(), "file:///w/a.go", Position{}); !errors.Is(err, ErrClosed) {
		t.Errorf("after crash: %v", err)
	}
}

func TestDefaultEncodingAndNoSync(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"textDocumentSync":0}`
	c := connect(t, f, r, w, nil)
	if c.Encoding() != UTF16 || c.HasDefinition() {
		t.Errorf("encoding %q, definition %v", c.Encoding(), c.HasDefinition())
	}
	c.DidOpen("file:///w/a.go", "go", 1, "x")
	c.DidChange("file:///w/a.go", 2, "y")
	c.Shutdown(time.Minute)
	for _, m := range f.methods() {
		if strings.HasPrefix(m, "textDocument/did") {
			t.Errorf("sent %s with sync none", m)
		}
	}
}

func TestParseHover(t *testing.T) {
	for raw, want := range map[string]string{
		`null`: "",
		`{"contents":{"kind":"markdown","value":"**x** int"}}`:       "**x** int",
		`{"contents":{"kind":"plaintext","value":"a*b"}}`:            "```text\na*b\n```",
		`{"contents":"just text"}`:                                   "just text",
		`{"contents":{"language":"go","value":"func F()"}}`:          "```go\nfunc F()\n```",
		`{"contents":["doc",{"language":"go","value":"var v int"}]}`: "doc\n\n---\n\n```go\nvar v int\n```",
		`{"contents":[]}`: "",
	} {
		got, _, err := ParseHover(json.RawMessage(raw))
		if err != nil || got != want {
			t.Errorf("%s: %q, %v; want %q", raw, got, err, want)
		}
	}
	_, r, _ := ParseHover(json.RawMessage(`{"contents":"x","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":4}}}`))
	if r == nil || r.End.Character != 4 {
		t.Errorf("range %v", r)
	}
	if _, _, err := ParseHover(json.RawMessage(`{"contents":5}`)); err == nil {
		t.Error("bad contents accepted")
	}
}
