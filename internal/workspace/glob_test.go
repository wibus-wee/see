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
