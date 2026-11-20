package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRenameCapability(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","renameProvider":{"prepareProvider":true}}`
	c := connect(t, f, r, w, nil)
	if !c.HasRename() || !c.HasPrepareRename() {
		t.Fatal("rename capabilities not read")
	}
	f.navigation = func(method string, params json.RawMessage) string {
		switch method {
		case "textDocument/prepareRename":
			return `{"range":{"start":{"line":5,"character":1},"end":{"line":5,"character":2}},"placeholder":"x"}`
		case "textDocument/rename":
			var p struct {
				TextDocument struct{ URI string } `json:"textDocument"`
				Position     struct{ Line, Character int }
				NewName      string `json:"newName"`
			}
			json.Unmarshal(params, &p)
			if p.NewName != "y" || p.Position.Line != 5 {
				t.Errorf("rename params: %s", params)
			}
			return `{"changes":{"file:///w/main.go":[{"range":{"start":{"line":5,"character":1},"end":{"line":5,"character":2}},"newText":"y"}]}}`
		}
		return "null"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	prep, err := c.PrepareRename(ctx, "file:///w/main.go", Position{Line: 5, Character: 1})
	if err != nil {
		t.Fatal(err)
	}
	if prep == nil || prep.Placeholder != "x" || prep.Range.Start.Line != 5 {
		t.Fatalf("prepareRename: %+v", prep)
	}
	edit, err := c.Rename(ctx, "file:///w/main.go", Position{Line: 5, Character: 1}, "y")
	if err != nil {
		t.Fatal(err)
	}
	edits := edit.EditsFor("file:///w/main.go")
	if len(edits) != 1 || edits[0].NewText != "y" {
		t.Fatalf("rename edit: %+v", edits)
	}
}

func TestRenameNoProvider(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8"}`
	c := connect(t, f, r, w, nil)
	if c.HasRename() || c.HasPrepareRename() {
		t.Fatal("rename should be off")
	}
}

func TestParsePrepareRename(t *testing.T) {
	tests := []struct {
		raw         string
		wantNil     bool
		placeholder string
		defBehavior bool
	}{
		{`null`, true, "", false},
		{`{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}},"placeholder":"foo"}`, false, "foo", false},
		{`{"start":{"line":1,"character":2},"end":{"line":1,"character":5}}`, false, "", false},
		{`{"defaultBehavior":true}`, false, "", true},
	}
	for _, tc := range tests {
		res, err := ParsePrepareRename(json.RawMessage(tc.raw))
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if tc.wantNil {
			if res != nil {
				t.Fatalf("%s: got %+v", tc.raw, res)
			}
			continue
		}
		if res == nil {
			t.Fatalf("%s: nil", tc.raw)
		}
		if res.Placeholder != tc.placeholder || res.DefaultBehavior != tc.defBehavior {
			t.Fatalf("%s: %+v", tc.raw, res)
		}
	}
	if _, err := ParsePrepareRename(json.RawMessage(`{bad`)); err == nil {
		t.Fatal("bad JSON should error")
	}
}

func TestPrepareRenameNullResult(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"renameProvider":{"prepareProvider":true}}`
	c := connect(t, f, r, w, nil)
	f.navigation = func(method string, params json.RawMessage) string { return "null" }
	_, err := c.PrepareRename(context.Background(), "file:///w/a.go", Position{})
	if !errors.Is(err, ErrNoRename) {
		t.Fatalf("err: %v", err)
	}
}
