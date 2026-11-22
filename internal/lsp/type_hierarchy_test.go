package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestTypeHierarchy(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"typeHierarchyProvider":{}}`
	f.navigation = func(method string, params json.RawMessage) string {
		switch method {
		case "textDocument/prepareTypeHierarchy":
			return `[{"name":"Reader","kind":11,"uri":"file:///a.go","tags":[1],"data":{"opaque":42}}]`
		case "typeHierarchy/supertypes", "typeHierarchy/subtypes":
			var p struct {
				Item TypeHierarchyItem `json:"item"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				t.Error(err)
			}
			if string(p.Item.Data) != `{"opaque":42}` || len(p.Item.Tags) != 1 {
				t.Errorf("lost item metadata: %s", params)
			}
			if method == "typeHierarchy/subtypes" {
				return "null"
			}
			return `[{"name":"Base","kind":11,"uri":"file:///b.go"}]`
		}
		return "null"
	}
	c := connect(t, f, r, w, nil)
	if !c.HasTypeHierarchy() {
		t.Fatal("capability not read")
	}
	ctx := context.Background()
	items, err := c.PrepareTypeHierarchy(ctx, "file:///a.go", Position{})
	if err != nil || len(items) != 1 {
		t.Fatalf("prepare: %v %v", items, err)
	}
	supers, err := c.Supertypes(ctx, items[0])
	if err != nil || len(supers) != 1 || supers[0].Name != "Base" {
		t.Fatalf("supertypes: %v %v", supers, err)
	}
	subs, err := c.Subtypes(ctx, items[0])
	if err != nil || len(subs) != 0 {
		t.Fatalf("subtypes: %v %v", subs, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.PrepareTypeHierarchy(ctx, "file:///a.go", Position{}); err == nil {
		t.Fatal("canceled request succeeded")
	}
}

func TestTypeHierarchyEmptyAndUnsupported(t *testing.T) {
	for _, cap := range []string{"true", "false", "null"} {
		t.Run(cap, func(t *testing.T) {
			f, r, w := newFake(t)
			f.caps = `{"typeHierarchyProvider":` + cap + `}`
			f.navigation = func(string, json.RawMessage) string { return "null" }
			c := connect(t, f, r, w, nil)
			_, err := c.PrepareTypeHierarchy(context.Background(), "file:///a.go", Position{})
			want := ErrTypeHierarchyUnsupported
			if cap == "true" {
				want = ErrNoTypeItem
			}
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
		})
	}
}
