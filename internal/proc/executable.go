package proc

import (
	"errors"
	"path/filepath"

	"see/internal/operr"
	"see/internal/workspace"
)

var ErrUntrustedExecutable = errors.New("workspace executable denied")

// ValidateExecutable checks a resolved launch candidate, including symlink targets.
func ValidateExecutable(root, executable string) error {
	if !filepath.IsAbs(executable) {
		return operr.New(operr.InvalidInput, operr.Permanent, errors.New("expected absolute executable path"))
	}
	real, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return operr.Classify(err)
	}
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
	if workspace.Within(root, executable) || workspace.Within(resolved, real) {
		return operr.New(operr.PermissionDenied, operr.Permanent, ErrUntrustedExecutable)
	}
	return nil
}
