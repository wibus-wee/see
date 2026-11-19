// Package arch tests the shape of the module: what lives where, and which
// package may import what. The Go compiler already refuses an import
// cycle; these rules keep the layers the design asks for, so that the tree
// does not quietly flatten back into one package.
package arch

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// root is the module root, two directories up from internal/arch.
var root = filepath.Join("..", "..")

func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names
}

// TestRootHoldsOnlyTheEntryPoint keeps the flat layout from coming back:
// the app's packages live under internal/, and the root holds app.go.
func TestRootHoldsOnlyTheEntryPoint(t *testing.T) {
	want := []string{"app.go"}
	if got := goFiles(t, "."); !slices.Equal(got, want) {
		t.Fatalf("the root holds %v, want only %v: move the package under internal/", got, want)
	}
}

// TestOnlyAppImportsTheWindowAPI keeps the app's state in one package: the
// packages under it may draw with mygo/ui, but a window, a menu, or the
// app's own event loop belongs to internal/app, through the port in
// internal/platform.
func TestOnlyAppImportsTheWindowAPI(t *testing.T) {
	for _, pkg := range internalPackages(t) {
		if pkg == "see/internal/app" || pkg == "see/internal/platform" {
			continue
		}
		info, err := build.ImportDir(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pkg, "see/"))), 0)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		for _, imp := range info.Imports {
			if imp == "github.com/egoist/mygo" {
				t.Errorf("%s imports github.com/egoist/mygo: the window API belongs to internal/app", pkg)
			}
		}
	}
}

// TestNobodyImportsApp keeps internal/app at the top of the tree: the
// packages under it are what it composes, and none of them may depend on
// it, which is what would make a cycle the next time one of them needed a
// window.
func TestNobodyImportsApp(t *testing.T) {
	for _, pkg := range internalPackages(t) {
		if pkg == "see/internal/app" {
			continue
		}
		info, err := build.ImportDir(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pkg, "see/"))), 0)
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		for _, imp := range info.Imports {
			if imp == "see/internal/app" {
				t.Errorf("%s imports internal/app: the app composes its packages, not the other way round", pkg)
			}
		}
	}
}

func TestChildCommandsUseProc(t *testing.T) {
	dirs := []string{"."}
	for _, pkg := range internalPackages(t) {
		if pkg != "see/internal/proc" {
			dirs = append(dirs, filepath.FromSlash(strings.TrimPrefix(pkg, "see/")))
		}
	}
	fset := token.NewFileSet()
	for _, dir := range dirs {
		for _, name := range goFiles(t, dir) {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, filepath.Join(root, dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range file.Imports {
				if imp.Path.Value != `"os/exec"` {
					continue
				}
				pkg := "exec"
				if imp.Name != nil {
					pkg = imp.Name.Name
				}
				if pkg == "." {
					t.Errorf("%s: do not dot-import os/exec; use internal/proc for child commands", fset.Position(imp.Pos()))
					continue
				}
				ast.Inspect(file, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext") {
						t.Errorf("%s: use internal/proc so the command does not open a Windows console", fset.Position(sel.Pos()))
					}
					return true
				})
			}
		}
	}
}

// internalPackages lists the packages under internal/, as import paths.
func internalPackages(t *testing.T) []string {
	t.Helper()
	var pkgs []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if len(goFilesOrNone(path)) == 0 {
			return nil
		}
		pkgs = append(pkgs, "see/"+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(pkgs)
	return pkgs
}

func goFilesOrNone(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			names = append(names, e.Name())
		}
	}
	return names
}
