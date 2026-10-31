package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// ripgrepArchive builds a release archive as ripgrep's are: a folder with
// rg inside it.
func ripgrepArchive(t *testing.T, asset string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if strings.HasSuffix(asset, ".zip") {
		t.Skip("the tar.gz archive is enough")
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho ripgrep\n")
	dir := "ripgrep-" + ripgrepVersion + "-" + strings.TrimSuffix(asset, ".tar.gz")
	if err := tw.WriteHeader(&tar.Header{Name: dir + "/", Typeflag: tar.TypeDir, Mode: 0755}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: dir + "/" + toolExe("rg"), Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveRipgrepRelease serves an archive and its checksum, as the release
// does, and points the installer at it.
func serveRipgrepRelease(t *testing.T, archive []byte, sum string) {
	t.Helper()
	asset, err := ripgrepAsset()
	if err != nil {
		t.Skip(err)
	}
	name := "ripgrep-" + ripgrepVersion + "-" + asset
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case name:
			w.Write(archive)
		case name + ".sha256":
			w.Write([]byte(sum))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := ripgrepReleaseURL
	ripgrepReleaseURL = srv.URL
	t.Cleanup(func() { ripgrepReleaseURL = old })
}

func checksum(data []byte) string {
	b := sha256.Sum256(data)
	return hex.EncodeToString(b[:])
}

func TestInstallRipgrepInstallsReleaseBinary(t *testing.T) {
	asset, err := ripgrepAsset()
	if err != nil {
		t.Skip(err)
	}
	archive := ripgrepArchive(t, asset)
	serveRipgrepRelease(t, archive, checksum(archive)+"  ripgrep.tar.gz\n")
	dest := filepath.Join(t.TempDir(), "tools", "ripgrep")
	if err := installRipgrep(context.Background(), dest); err != nil {
		t.Fatal(err)
	}
	rg := filepath.Join(dest, toolExe("rg"))
	fi, err := os.Stat(rg)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0100 == 0 {
		t.Fatalf("rg is not executable: %v", fi.Mode())
	}
	// A second installation does not replace the first.
	if err := installRipgrep(context.Background(), dest); err == nil {
		t.Fatal("overwrote an existing installation")
	}
}

func TestInstallRipgrepRejectsAnotherChecksum(t *testing.T) {
	asset, err := ripgrepAsset()
	if err != nil {
		t.Skip(err)
	}
	archive := ripgrepArchive(t, asset)
	serveRipgrepRelease(t, archive, checksum([]byte("something else"))+"  ripgrep.tar.gz\n")
	parent := t.TempDir()
	err = installRipgrep(context.Background(), filepath.Join(parent, "ripgrep"))
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("error = %v", err)
	}
	if entries, err := os.ReadDir(parent); err != nil || len(entries) != 0 {
		t.Fatalf("a failed installation left files: %v, %v", entries, err)
	}
}

func TestFindRipgrepUsesManagedCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SEE_RG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("PATH", t.TempDir()) // no ripgrep on the path
	path := managedRipgrepPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := findRipgrep("")
	if err != nil || got != path {
		t.Fatalf("path %q, error %v", got, err)
	}
	// $SEE_RG still wins over the copy See installed.
	other := filepath.Join(t.TempDir(), "rg")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_RG", other)
	if got, err := findRipgrep(""); err != nil || got != other {
		t.Fatalf("path %q, error %v", got, err)
	}
}

func TestFindGoplsUsesManagedCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SEE_GOPLS", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GOPATH", "")
	t.Setenv("GOBIN", "")
	t.Setenv("HOME", home)
	path := managedGoplsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := findGopls(""); err != nil || got != path {
		t.Fatalf("path %q, error %v", got, err)
	}
}

func TestInstallGoplsBuildsWithGoToolchain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"$@\" > \"$GOBIN/args\"\nmkdir -p \"$GOBIN\"\nprintf gopls > \"$GOBIN/gopls\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	dest := filepath.Join(t.TempDir(), "tools", "gopls")
	if err := installGopls(context.Background(), dest); err != nil {
		t.Fatal(err)
	}
	if !regularFile(filepath.Join(dest, "bin", "gopls")) {
		t.Fatal("gopls was not installed")
	}
	args, err := os.ReadFile(filepath.Join(dest, "bin", "args"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(args)) != "install golang.org/x/tools/gopls@"+goplsVersion {
		t.Fatalf("go install arguments %q", args)
	}
	// Without a Go toolchain, the row says what is missing.
	t.Setenv("PATH", t.TempDir())
	err = installGopls(context.Background(), filepath.Join(t.TempDir(), "gopls"))
	if err == nil || !strings.Contains(err.Error(), "install Go first") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallingRipgrepFromSettingsKeepsIt(t *testing.T) {
	asset, err := ripgrepAsset()
	if err != nil {
		t.Skip(err)
	}
	archive := ripgrepArchive(t, asset)
	serveRipgrepRelease(t, archive, checksum(archive)+"  ripgrep.tar.gz\n")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("SEE_RG", "")
	t.Setenv("PATH", t.TempDir())
	h := newHarness(t, t.TempDir())
	h.a.settings.pane = "search"
	st := ui.NewTester(h.a.settingsView, 900, 680)
	if !st.HasText("Install ripgrep") {
		t.Fatalf("no install button: %q", st.Texts())
	}
	if err := st.Click("Install ripgrep"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.serverJob })
	for range 5 {
		st.Frame()
	}
	if !managedRipgrepInstalled() {
		t.Fatalf("ripgrep was not installed: %q", st.Texts())
	}
	if !st.HasText("Remove managed ripgrep") {
		t.Fatalf("missing installed state: %v", st.Texts())
	}
	if got, err := findRipgrep(""); err != nil || got != managedRipgrepPath() {
		t.Fatalf("path %q, error %v", got, err)
	}
	if err := st.Click("Remove managed ripgrep"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.serverJob })
	for range 5 {
		st.Frame()
	}
	if managedRipgrepInstalled() {
		t.Fatal("ripgrep was not removed")
	}
}

func TestInstallingGoplsFromSettingsBuildsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("SEE_GOPLS", "")
	t.Setenv("GOPATH", "")
	t.Setenv("GOBIN", "")
	bin := t.TempDir()
	script := "#!/bin/sh\nmkdir -p \"$GOBIN\"\nprintf gopls > \"$GOBIN/gopls\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	h.a.settings.pane = "intelligence"
	st := ui.NewTester(h.a.settingsView, 900, 680)
	if !st.HasText("Install gopls") {
		t.Fatalf("no install button: %q", st.Texts())
	}
	if err := st.Click("Install gopls"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.serverJob })
	for range 5 {
		st.Frame()
	}
	if !managedGoplsInstalled() {
		t.Fatalf("gopls was not installed: %q", st.Texts())
	}
	if !st.HasText("Remove managed gopls") {
		t.Fatalf("missing installed state: %v", st.Texts())
	}
	if got, err := findGopls(h.a.root); err != nil || got != managedGoplsPath() {
		t.Fatalf("path %q, error %v", got, err)
	}
}
