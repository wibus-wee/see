package workspace

import "testing"

func TestMatchVSCodeGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, rel string
		want         bool
	}{
		{"build", "build", true},
		{"build", "nested/build", false},
		{"**/build", "nested/build", true},
		{"**/build", "build", true},
		{"*.js", "main.js", true},
		{"*.js", "src/main.js", false},
		{"**/*.{ts,js}", "src/main.ts", true},
		{"**/*.{ts,js}", "src/main.js", true},
		{"**/*.{ts,js}", "src/main.go", false},
		{"src/generated/**", "src/generated/a/b.txt", true},
		{"src/generated/**", "src/generated", true},
		{"src/**/test_?.go", "src/a/b/test_1.go", true},
		{"/build", "build", true},
		{"build/", "build", true},
	} {
		if got := MatchVSCodeGlob(tc.pattern, tc.rel); got != tc.want {
			t.Errorf("MatchVSCodeGlob(%q, %q) = %v, want %v", tc.pattern, tc.rel, got, tc.want)
		}
	}
}

func TestVSCodeExcludeExceptions(t *testing.T) {
	patterns := map[string]bool{"node_modules": true, "!node_modules/@types": true, "!**/generated/keep.txt": true}
	for rel, want := range map[string]bool{
		"node_modules/pkg/index.js":    true,
		"node_modules/@types/pkg.d.ts": false,
		"src/generated/drop.txt":       true,
		"src/generated/keep.txt":       false,
		"nested/generated/keep.txt":    false,
	} {
		patterns["**/generated/**"] = true
		if got := Excluded(patterns, rel); got != want {
			t.Errorf("Excluded(%q) = %v, want %v", rel, got, want)
		}
	}
	if !ExceptionMayReveal(patterns, "node_modules") || ExceptionMayReveal(map[string]bool{"!other/keep": true}, "node_modules") {
		t.Error("exception directory reveal predicate")
	}
}

func TestConditionalDirectoryExclusionCoversDescendants(t *testing.T) {
	clauses := map[string]string{"**/dist": "$(basename).marker"}
	hasSibling := func(rel string) bool { return rel == "dist.marker" }
	if !ExcludedWithSiblings(nil, clauses, "dist/index.js", hasSibling) {
		t.Error("conditional directory exclusion did not cover its descendant")
	}
}
