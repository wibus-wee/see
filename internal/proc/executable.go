package proc

import (
	"errors"
	"path/filepath"

	"see/internal/operr"
	"see/internal/workspace"
)

var ErrUntrustedExecutable = errors.New("workspace executable denied")

// ValidateExecutable checks a resolved launch candidate, including
// symlink targets. Comparison is by the disk's own spelling of what
// exists: EvalSymlinks resolves links but not the volume's case or
// Unicode normalization, so on a case-insensitive file system a wrongly
// cased path into the workspace names the workspace's bytes while
// differing lexically — denying only the exact spelling would execute
// the same executable through its variant.
func ValidateExecutable(root, executable string) error {
	if !filepath.IsAbs(executable) {
		return operr.New(operr.InvalidInput, operr.Permanent, errors.New("expected absolute executable path"))
	}
	real, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return operr.Classify(err)
	}
	real = workspace.DiskSpelling(real)
	spelled := workspace.DiskSpelling(executable)
	if root == "" {
		return nil
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	root = workspace.DiskSpelling(root)
	resolved = workspace.DiskSpelling(resolved)
	if workspace.Within(root, spelled) || workspace.Within(resolved, real) {
		return operr.New(operr.PermissionDenied, operr.Permanent, ErrUntrustedExecutable)
	}
	return nil
}
