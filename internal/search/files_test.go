package search

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFileQueriesMatchFullPaths(t *testing.T) {
	files := []string{"packages/runtime/src/index.ts", "packages/eval/src/runtime-evidence.ts", "other/runtime.ts", "runtime/package.json", "目录/runtime/你好.go"}
	for _, query := range []string{"runtime package", "package runtime", " runtime\tpackage "} {
		got := Files(files, query, 10)
		if len(got) != 3 {
			t.Fatalf("%q: %+v", query, got)
		}
		for _, result := range got {
			if result.Path == "other/runtime.ts" {
				t.Fatal("accepted missing term")
			}
		}
	}
	got := Files(files, "packages/runtime", 10)
	if len(got) == 0 || got[0].Path != files[0] {
		t.Fatalf("ordered path: %+v", got)
	}
	for _, query := range []string{"目录/ru", "你好 目录"} {
		got = Files(files, query, 10)
		if len(got) != 1 || got[0].Path != files[4] {
			t.Fatalf("Unicode %q: %+v", query, got)
		}
		for _, hit := range got[0].Hits {
			if hit < 0 || hit >= len(got[0].Path) || !utf8.RuneStart(got[0].Path[hit]) {
				t.Fatalf("invalid byte hit: %+v", got)
			}
		}
	}
	if got := Files(files, "runtime package absent", 10); len(got) != 0 {
		t.Fatalf("missing term: %+v", got)
	}
	if got := Files(files, "runtime package", 1); len(got) != 1 {
		t.Fatalf("limit: %+v", got)
	}
	if got := Files(files, "runtime", 0); len(got) != 0 {
		t.Fatal("zero limit")
	}
	if got := Files(files, " \t", 2); len(got) != 2 || got[0].Path != files[0] {
		t.Fatal("empty query")
	}
	plain := Files([]string{"packages/main.go", "main.go", "main/other.go"}, "main", 10)
	if plain[0].Path != "main.go" {
		t.Fatalf("basename preference: %+v", plain)
	}
	hits := Files([]string{"packages/runtime/src/index.ts"}, "runtime package", 1)[0].Hits
	if !slices.IsSorted(hits) || len(slices.Compact(slices.Clone(hits))) != len(hits) {
		t.Fatalf("unordered/duplicate hits %v", hits)
	}
	var matched strings.Builder
	for _, hit := range hits {
		matched.WriteByte("packages/runtime/src/index.ts"[hit])
	}
	if matched.String() != "packageruntime" {
		t.Fatalf("directory hits %q", matched.String())
	}
}
