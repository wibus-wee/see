package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"see/internal/proc"
)

// ripgrepArchive builds a release archive as ripgrep's are: a folder with
// rg inside it.
func ripgrepArchive(t *testing.T, asset string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if strings.HasSuffix(asset, ".zip") {
		zw := zip.NewWriter(&buf)
		header := &zip.FileHeader{Name: "ripgrep-" + ripgrepVersion + "/" + toolExe("rg")}
		header.SetMode(0755)
		file, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte("ripgrep fixture")); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
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
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0100 == 0 {
		t.Fatalf("rg is not executable: %v", fi.Mode())
	}
	// A second installation does not replace the first.
	if err := installRipgrep(context.Background(), dest); err == nil {
		t.Fatal("overwrote an existing installation")
	}
}

// certUtilChecksum is a checksum file as ripgrep's Windows releases ship
// it: CertUtil's output, with CRLF line endings.
func certUtilChecksum(data []byte, name string) string {
	return "SHA256 hash of " + name + ":\r\n" + checksum(data) + "\r\nCertUtil: -hashfile command completed successfully.\r\n"
}

func TestInstallRipgrepAcceptsCertUtilChecksum(t *testing.T) {
	asset, err := ripgrepAsset()
	if err != nil {
		t.Skip(err)
	}
	archive := ripgrepArchive(t, asset)
	serveRipgrepRelease(t, archive, certUtilChecksum(archive, "ripgrep-"+ripgrepVersion+"-"+asset))
	dest := filepath.Join(t.TempDir(), "ripgrep")
	if err := installRipgrep(context.Background(), dest); err != nil {
		t.Fatal(err)
	}
	if !regularFile(filepath.Join(dest, toolExe("rg"))) {
		t.Fatal("rg was not installed")
	}
}

func TestVerifyChecksumFormats(t *testing.T) {
	data := []byte("ripgrep")
	sum := checksum(data)
	spaced := ""
	for i := 0; i < len(sum); i += 2 {
		spaced += sum[i:i+2] + " "
	}
	other := checksum([]byte("something else"))
	for _, tc := range []struct {
		name, file string
		ok         bool
	}{
		{"unix", sum + "  ripgrep.tar.gz\n", true},
		{"uppercase", strings.ToUpper(sum) + "  ripgrep.tar.gz\n", true},
		{"certutil", certUtilChecksum(data, "ripgrep.zip"), true},
		{"certutil bytes", "SHA256 hash of file ripgrep.zip:\r\n" + spaced + "\r\nCertUtil: -hashfile command completed successfully.\r\n", true},
		{"another hash", other + "  ripgrep.tar.gz\n", false},
		{"certutil another hash", certUtilChecksum([]byte("something else"), "ripgrep.zip"), false},
		{"empty", "", false},
		{"no hash", "SHA256 hash of ripgrep.zip:\r\nCertUtil: -hashfile command failed\r\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyChecksum(data, []byte(tc.file))
			if (err == nil) != tc.ok {
				t.Fatalf("verifyChecksum = %v, want ok=%v", err, tc.ok)
			}
		})
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

func TestInstallRipgrepChecksumMismatchCannotReachTheToolDirectory(t *testing.T) {
	// REQ-CHK-020/030 at the install entrypoint: whatever the server
	// serves, a checksum mismatch means no ripgrep is staged into place
	// — nothing executable appears, and nothing partial is left to be
	// executed by mistake or to defeat the next attempt's freshness
	// check.
	asset, err := ripgrepAsset()
	if err != nil {
		t.Skip(err)
	}
	archive := ripgrepArchive(t, asset)

	// First the mirror serves its true release, and the install lands.
	serveRipgrepRelease(t, archive, checksum(archive)+"  ripgrep.tar.gz\n")
	parent := t.TempDir()
	dest := filepath.Join(parent, "ripgrep")
	if err := installRipgrep(context.Background(), dest); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(filepath.Join(dest, toolExe("rg")))
	if err != nil {
		t.Fatal(err)
	}

	// A reinstall is refused before anything is even downloaded: the
	// existing installation can never be harmed by what a mirror
	// serves, because nothing it serves is ever applied over one. This
	// is half of REQ-CHK-030 — corruption cannot displace a working
	// tool because nothing ever writes over one.
	serveRipgrepRelease(t, archive, checksum([]byte("something else"))+"  ripgrep.tar.gz\n")
	if err := installRipgrep(context.Background(), dest); err == nil {
		t.Fatal("reinstalling over an existing installation was not refused")
	}
	data, err := os.ReadFile(filepath.Join(dest, toolExe("rg")))
	if err != nil {
		t.Fatalf("the installed ripgrep vanished: %v", err)
	}
	if !bytes.Equal(data, installed) {
		t.Fatal("the installed ripgrep was modified")
	}

	// On an empty directory, the corrupted mirror's archive never
	// becomes executable either: no staging directory survives to be
	// mistaken for an installation.
	fresh := filepath.Join(parent, "fresh", "ripgrep")
	if err := installRipgrep(context.Background(), fresh); err == nil {
		t.Fatal("the corrupted mirror's archive was installed")
	}
	entries, err := os.ReadDir(filepath.Dir(fresh))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the failed install left files: %v", entries)
	}
}

func TestFindRipgrepUsesManagedCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", filepath.Join(home, "config"))
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

func TestRipgrepArchivesExtractExpectedBinary(t *testing.T) {
	for _, asset := range []string{"fixture.tar.gz", "fixture.zip"} {
		t.Run(asset, func(t *testing.T) {
			data := ripgrepArchive(t, asset)
			got, err := ripgrepBinary(data, asset)
			if err != nil {
				t.Fatal(err)
			}
			want := "#!/bin/sh\necho ripgrep\n"
			if strings.HasSuffix(asset, ".zip") {
				want = "ripgrep fixture"
			}
			if string(got) != want {
				t.Fatalf("binary %q, want %q", got, want)
			}
			if _, err := ripgrepBinary(data[:len(data)/2], asset); err == nil {
				t.Fatal("accepted truncated archive")
			}
		})
	}
}

func TestFindRejectsWorkspaceRipgrepAlias(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, toolExe("rg"))
	if err := os.WriteFile(path, []byte("workspace program"), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), toolExe("rg"))
	if err := os.Symlink(path, alias); err != nil {
		t.Skip(err)
	}
	t.Setenv("SEE_RG", alias)
	if got, err := findRipgrep(root); got != "" || !errors.Is(err, proc.ErrUntrustedExecutable) {
		t.Fatalf("findRipgrep returned %q, %v", got, err)
	}
}
