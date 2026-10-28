package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// canonicalRoot makes a workspace's path absolute and clean, without
// resolving symlinks, so the tree shows the path the user chose.
func canonicalRoot(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", &os.PathError{Op: "open", Path: abs, Err: errNotDir}
	}
	return filepath.Clean(abs), nil
}

var errNotDir = errors.New("not a directory")

// within reports whether p is root or inside it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// docPath is the identity of the document of a path: the path clean and
// absolute, and, for a path a language server gives through the real
// location of a symlinked workspace (as macOS's /tmp is /private/tmp),
// the same path under the workspace as the user opened it.
func docPath(root, realRoot, p string) string {
	p = filepath.Clean(p)
	if root != "" && realRoot != "" && realRoot != root && within(realRoot, p) && !within(root, p) {
		if rel, err := filepath.Rel(realRoot, p); err == nil {
			return filepath.Join(root, rel)
		}
	}
	return p
}

// relPath is p relative to root for showing, or p itself outside it.
func relPath(root, p string) string {
	if root != "" && within(root, p) {
		if rel, err := filepath.Rel(root, p); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return p
}
