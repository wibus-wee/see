package lsp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
)

func TestCodeActionsRequest(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","codeActionProvider":{"codeActionKinds":["quickfix","refactor"],"resolveProvider":true},"executeCommandProvider":{"commands":["x"]}}`
	c := connect(t, f, r, w, func(error) {})
	if !c.HasCodeActions() || !c.HasExecuteCommand() {
		t.Fatal("capabilities not read")
	}
	f.navigation = func(method string, params json.RawMessage) string {
		switch method {
		case "textDocument/codeAction":
			var p struct {
				TextDocument struct{ URI string } `json:"textDocument"`
				Range        struct {
					Start struct{ Line, Character int }
					End   struct{ Line, Character int }
				} `json:"range"`
				Context struct {
					Diagnostics []struct{ Message string } `json:"diagnostics"`
				} `json:"context"`
			}
			json.Unmarshal(params, &p)
			if p.Range.Start.Line != 3 || len(p.Context.Diagnostics) != 1 || p.Context.Diagnostics[0].Message != "bad" {
				t.Errorf("params: %s", params)
			}
			return `[{"title":"Fix","kind":"quickfix","isPreferred":true,"edit":{"changes":{"file:///w/a.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"x"}]}}},{"title":"Cmd","command":{"title":"Cmd","command":"x"}}]`
		}
		return "null"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diags := []Diagnostic{{Message: "bad"}}
	actions, err := c.CodeActions(ctx, "file:///w/a.go", Range{Start: Position{Line: 3, Character: 0}, End: Position{Line: 3, Character: 8}}, diags)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[0].Title != "Fix" || actions[0].Edit == nil {
		t.Fatalf("actions: %+v", actions)
	}
	if actions[1].Command == nil || actions[1].Command.Command != "x" {
		t.Fatalf("command action: %+v", actions[1])
	}
}

func TestCodeActionUnmarshal(t *testing.T) {
	var actions []CodeAction
	err := json.Unmarshal([]byte(`[
		{"title":"Fix","kind":"quickfix","isPreferred":true,"edit":{"changes":{"file:///a":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"x"}]}}},
		{"title":"Cmd","command":{"title":"Cmd","command":"x","arguments":[1]}},
		{"title":"Legacy","command":"y","arguments":[2]},
		{"title":"Lazy","kind":"refactor","data":{"id":9}},
		{"title":"Off","disabled":{"reason":"not now"}}
	]`), &actions)
	if err != nil {
		t.Fatal(err)
	}
	if actions[0].Edit == nil || actions[0].Command != nil || !actions[0].IsPreferred {
		t.Errorf("action 0: %+v", actions[0])
	}
	if actions[1].Command == nil || actions[1].Command.Command != "x" {
		t.Errorf("action 1: %+v", actions[1])
	}
	if actions[2].Command == nil || actions[2].Command.Command != "y" {
		t.Errorf("action 2 (command-shaped reply): %+v", actions[2])
	}
	if !actions[3].NeedsResolve() || actions[3].Data == nil {
		t.Errorf("action 3 (resolve): %+v", actions[3])
	}
	if actions[4].Disabled != "not now" {
		t.Errorf("action 4: %+v", actions[4])
	}
}

func TestCodeActionResolveRoundTrip(t *testing.T) {
	var action CodeAction
	if err := json.Unmarshal([]byte(`{"title":"Lazy","kind":"refactor","data":{"id":9}}`), &action); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(action)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	json.Unmarshal(b, &back)
	if back["title"] != "Lazy" || back["kind"] != "refactor" {
		t.Fatalf("marshal dropped fields: %s", b)
	}
	if _, ok := back["data"]; !ok {
		t.Fatalf("marshal dropped data: %s", b)
	}
}

func TestWorkspaceApplyEdit(t *testing.T) {
	f, r, w := newFake(t)
	c := connect(t, f, r, w, func(error) {})
	var got *WorkspaceEdit
	done := make(chan struct{})
	c.SetApplyEditHandler(func(edit *WorkspaceEdit) error {
		got = edit
		close(done)
		return nil
	})
	// A server request arrives on the client's read loop; the answer
	// comes back on the wire as a response the fake server reads.
	f.send(`{"jsonrpc":"2.0","id":"s9","method":"workspace/applyEdit","params":{"edit":{"changes":{"file:///w/a.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"x"}]}}}}`)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("applyEdit never called the handler")
	}
	if got == nil || len(got.Changes["file:///w/a.go"]) != 1 {
		t.Fatalf("edit: %+v", got)
	}
	// The response to s9 lands in replies with applied:true.
	select {
	case body := <-f.replies:
		var reply struct {
			ID     string `json:"id"`
			Result struct {
				Applied bool `json:"applied"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID != "s9" || !reply.Result.Applied {
			t.Fatalf("reply: %s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reply to applyEdit")
	}
}

func TestWorkspaceApplyEditNoHandler(t *testing.T) {
	f, r, w := newFake(t)
	_ = connect(t, f, r, w, func(error) {})
	// With no handler registered the request is refused, as a client
	// that advertised no applyEdit should.
	f.send(`{"jsonrpc":"2.0","id":"s9","method":"workspace/applyEdit","params":{"edit":{}}}`)
	select {
	case body := <-f.replies:
		var reply struct {
			Error *struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Error == nil || reply.Error.Code != CodeMethodNotFound {
			t.Fatalf("reply: %s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
}

func TestCodeActionsInitializeAdvertises(t *testing.T) {
	f, r, w := newFake(t)
	_ = connect(t, f, r, w, func(error) {})
	var params struct {
		Capabilities struct {
			TextDocument struct {
				CodeAction *struct {
					LiteralSupport *struct {
						CodeActionKind struct {
							ValueSet []string `json:"valueSet"`
						} `json:"codeActionKind"`
					} `json:"codeActionLiteralSupport"`
					ResolveSupport *struct {
						Properties []string `json:"properties"`
					} `json:"resolveSupport"`
					DataSupport bool `json:"dataSupport"`
				} `json:"codeAction"`
			} `json:"textDocument"`
			Workspace struct {
				ApplyEdit     bool `json:"applyEdit"`
				WorkspaceEdit *struct {
					DocumentChanges bool `json:"documentChanges"`
				} `json:"workspaceEdit"`
				ExecuteCommand *struct{} `json:"executeCommand"`
			} `json:"workspace"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(f.initializeParams, &params); err != nil {
		t.Fatal(err)
	}
	ca := params.Capabilities.TextDocument.CodeAction
	if ca == nil || ca.LiteralSupport == nil || !ca.DataSupport {
		t.Fatalf("codeAction capabilities: %s", f.initializeParams)
	}
	quickfix := false
	for _, k := range ca.LiteralSupport.CodeActionKind.ValueSet {
		if k == "quickfix" {
			quickfix = true
		}
	}
	if !quickfix {
		t.Fatalf("valueSet: %+v", ca.LiteralSupport.CodeActionKind.ValueSet)
	}
	found := false
	for _, p := range ca.ResolveSupport.Properties {
		if p == "edit" || p == "command" {
			found = true
		}
	}
	if !found {
		t.Fatalf("resolveSupport: %+v", ca.ResolveSupport)
	}
	ws := params.Capabilities.Workspace
	if !ws.ApplyEdit || ws.WorkspaceEdit == nil || !ws.WorkspaceEdit.DocumentChanges {
		t.Fatalf("workspace capabilities: %s", f.initializeParams)
	}
	if ws.ExecuteCommand == nil {
		t.Fatal("executeCommand capability not advertised")
	}
}

func TestApplyTextEdits(t *testing.T) {
	text := "package main\n\nfunc f() {}\n"
	snap := editor.NewBuffer(text).Snapshot()
	edits := []TextEdit{
		{Range: Range{Start: Position{Line: 2, Character: 5}, End: Position{Line: 2, Character: 6}}, NewText: "g"},
		{Range: Range{Start: Position{Line: 0, Character: 8}, End: Position{Line: 0, Character: 12}}, NewText: "demo"},
	}
	got := ApplyTextEdits(snap, edits, UTF16)
	if len(got) != 2 {
		t.Fatalf("edits: %+v", got)
	}
	// Sorted by start so applying them back to front shifts nothing.
	if got[0].Start > got[1].Start {
		t.Fatal("not sorted")
	}
	out := text
	for i := len(got) - 1; i >= 0; i-- {
		e := got[i]
		out = out[:e.Start] + e.Text + out[e.End:]
	}
	if out != "package demo\n\nfunc g() {}\n" {
		t.Fatalf("result: %q", out)
	}
}

func TestApplyTextEditsUTF8Columns(t *testing.T) {
	text := "héllo wörld\n"
	snap := editor.NewBuffer(text).Snapshot()
	// UTF-8 columns are byte offsets: "wörld" starts at byte 7 (é is
	// two bytes) and ends at 12.
	edits := []TextEdit{
		{Range: Range{Start: Position{Line: 0, Character: 7}, End: Position{Line: 0, Character: 13}}, NewText: "there"},
	}
	got := ApplyTextEdits(snap, edits, UTF8)
	if len(got) != 1 {
		t.Fatalf("edits: %+v", got)
	}
	out := text[:got[0].Start] + got[0].Text + text[got[0].End:]
	if out != "héllo there\n" {
		t.Fatalf("result: %q", out)
	}
}
