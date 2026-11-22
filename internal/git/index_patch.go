package git

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// PathChange reads the current status of one path, including conflicts and
// untracked files. A clean or ignored path has an empty Change.
func PathChange(ctx context.Context, root, name string) (Change, error) {
	out, err := Command(ctx, root, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return Change{}, err
	}
	for _, ch := range parseEntriesV2(string(out)) {
		if ch.Path == name {
			return ch, nil
		}
	}
	return Change{}, nil
}

// ChangePatch returns a full-context, byte-preserving patch for one path.
func ChangePatch(ctx context.Context, root, name string, staged, untracked bool) ([]byte, error) {
	args := []string{"-c", "diff.suppressBlankEmpty=false", "diff", "--no-ext-diff", "--no-textconv", "--no-color",
		"--src-prefix=a/", "--dst-prefix=b/", "--full-index", "--unified=2147483647"}
	var env []string
	if untracked {
		// Let Git apply its usual attributes and clean filters without
		// adding the file to the real index just to preview it.
		dir, err := os.MkdirTemp("", "see-git-index-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		env = []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
		if _, stderr, err := commandOutputEnv(ctx, root, env, "add", "--intent-to-add", "--", name); err != nil {
			return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(stderr)))
		}
	}
	if staged {
		args = append(args, "--cached")
	}
	out, stderr, err := commandOutputEnv(ctx, root, env, append(args, "--", name)...)
	if err != nil {
		return out, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(stderr)))
	}
	return out, nil
}

// ApplyIndexText updates only the index. An empty mode denotes an absent path;
// an empty line slice with a nonempty mode denotes a tracked empty file.
// Each line includes its ending, except the final line if it has none.
func ApplyIndexText(ctx context.Context, root, name, oldMode, newMode string, old, next []string) error {
	if name == "" || name == "." || name == ".." || path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") ||
		!indexTextMode(oldMode) || !indexTextMode(newMode) || oldMode == "" && newMode == "" || oldMode != "" && newMode != "" && oldMode != newMode {
		return fmt.Errorf("cannot apply a text patch to %q", name)
	}
	a, b := indexPatchPath("a/"+name), indexPatchPath("b/"+name)
	var patch strings.Builder
	fmt.Fprintf(&patch, "diff --git %s %s\n", a, b)
	switch {
	case oldMode == "":
		fmt.Fprintf(&patch, "new file mode %s\n", newMode)
		a = "/dev/null"
	case newMode == "":
		fmt.Fprintf(&patch, "deleted file mode %s\n", oldMode)
		b = "/dev/null"
	}
	if len(old)+len(next) > 0 {
		// Cover the entire preimage, with no offset or reduced context,
		// so a concurrent index edit rejects the patch rather than rebasing it.
		fmt.Fprintf(&patch, "--- %s\n+++ %s\n@@ -%d,%d +%d,%d @@\n", a, b, rangeStart(old), len(old), rangeStart(next), len(next))
		for _, side := range []struct {
			prefix byte
			lines  []string
		}{{'-', old}, {'+', next}} {
			for _, line := range side.lines {
				patch.WriteByte(side.prefix)
				patch.WriteString(line)
				if !strings.HasSuffix(line, "\n") {
					patch.WriteString("\n\\ No newline at end of file\n")
				}
			}
		}
	}
	_, err := commandInput(ctx, root, []byte(patch.String()), "-c", "apply.ignoreWhitespace=false", "apply", "--cached", "--whitespace=nowarn", "-")
	return err
}

func indexTextMode(mode string) bool { return mode == "" || mode == "100644" || mode == "100755" }

func rangeStart(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	return 1
}

func indexPatchPath(name string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := range len(name) {
		c := name[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c >= 0x7f:
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
