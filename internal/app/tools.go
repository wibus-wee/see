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
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/langserver"
	"see/internal/languages"
	"see/internal/search"
	"see/internal/shellpath"
)

// Tools See installs itself, so a tool can be had from Settings without a
// package manager. Their copies live in the application data folder beside
// the language servers See installs, and the workspace is never written to.
// As with tsrs, $SEE_RG or $SEE_GOPLS comes first, then See's copy, then
// PATH: remove the copy to go back to your own.

// The versions See downloads. Bump one and the next installation takes it;
// an installed copy is left alone until it is removed and installed again.
const (
	ripgrepVersion = "15.2.0"
	goplsVersion   = "v0.24.0"
)

// installTimeout is how long an installation may take: a download, or a
// build of gopls by the Go toolchain.
const installTimeout = 10 * time.Minute

// ripgrepReleaseURL is where ripgrep's releases are downloaded from; a
// test points it at its own server.
var ripgrepReleaseURL = "https://github.com/BurntSushi/ripgrep/releases/download"

// downloadLimit keeps a download out of memory's way when a server answers
// with something other than a release.
const downloadLimit = 64 << 20

// appDataDir is See's folder in the user's application data, holding the
// tools and language servers it manages.
func appDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "See")
}

func managedToolsDir() string {
	base := appDataDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "tools")
}

// toolExe is name as this platform spells its executables.
func toolExe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// ----- ripgrep -----

// managedRipgrepDir contains only See's ripgrep installation.
func managedRipgrepDir() string {
	dir := managedToolsDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "ripgrep")
}

func managedRipgrepPath() string {
	dir := managedRipgrepDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, toolExe("rg"))
}

func managedRipgrepExists() bool {
	dir := managedRipgrepDir()
	if dir == "" {
		return false
	}
	_, err := os.Lstat(dir)
	return err == nil
}

func managedRipgrepInstalled() bool {
	return regularFile(managedRipgrepPath())
}

// findRipgrep finds the ripgrep See searches with: one named by $SEE_RG or
// on PATH, or the copy See installed, which comes first after $SEE_RG.
func findRipgrep(root string) (string, error) {
	return search.FindRipgrep(root, managedRipgrepPath())
}

// ripgrepAsset is the name of ripgrep's release asset for this platform.
func ripgrepAsset() (string, error) {
	arch := map[string]string{
		"arm64": "aarch64",
		"amd64": "x86_64",
	}[runtime.GOARCH]
	switch runtime.GOOS {
	case "darwin":
		if arch != "" {
			return arch + "-apple-darwin.tar.gz", nil
		}
	case "linux":
		if arch != "" {
			// Static builds, which run on any distribution.
			return arch + "-unknown-linux-musl.tar.gz", nil
		}
	case "windows":
		if arch != "" {
			return arch + "-pc-windows-msvc.zip", nil
		}
	}
	return "", fmt.Errorf("no ripgrep release for %s on %s", runtime.GOARCH, runtime.GOOS)
}

// installRipgrep downloads ripgrep's release for this platform, checks it
// against the release's checksum, and stages it before it is exposed.
func installRipgrep(ctx context.Context, dest string) error {
	if err := shellpath.Wait(ctx); err != nil {
		return err
	}
	if dest == "" {
		return errors.New("cannot locate the application data folder")
	}
	if _, err := os.Lstat(dest); err == nil {
		return errors.New("a ripgrep installation already exists; remove it first")
	} else if !os.IsNotExist(err) {
		return err
	}
	asset, err := ripgrepAsset()
	if err != nil {
		return err
	}
	url := ripgrepReleaseURL + "/" + ripgrepVersion + "/ripgrep-" + ripgrepVersion + "-" + asset
	archive, err := fetch(ctx, url, downloadLimit)
	if err != nil {
		return err
	}
	sum, err := fetch(ctx, url+".sha256", 4096)
	if err != nil {
		return err
	}
	if err := verifyChecksum(archive, sum); err != nil {
		return err
	}
	binary, err := ripgrepBinary(archive, asset)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dest), ".ripgrep-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.WriteFile(filepath.Join(stage, toolExe("rg")), binary, 0700); err != nil {
		return err
	}
	return os.Rename(stage, dest)
}

// ripgrepBinary takes rg out of the release archive.
func ripgrepBinary(archive []byte, asset string) ([]byte, error) {
	want := toolExe("rg")
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, errors.New("the download is not a ZIP archive")
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) != want {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return readLimited(rc)
		}
		return nil, errors.New("the download did not contain ripgrep")
	}
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, errors.New("the download is not a compressed archive")
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("the download is not a tar archive")
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == want {
			return readLimited(tr)
		}
	}
	return nil, errors.New("the download did not contain ripgrep")
}

// ----- gopls -----

// managedGoplsDir contains only See's gopls installation, built by the Go
// toolchain into its bin folder.
func managedGoplsDir() string {
	dir := managedToolsDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "gopls")
}

func managedGoplsPath() string {
	dir := managedGoplsDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "bin", toolExe("gopls"))
}

func managedGoplsExists() bool {
	dir := managedGoplsDir()
	if dir == "" {
		return false
	}
	_, err := os.Lstat(dir)
	return err == nil
}

func managedGoplsInstalled() bool {
	return regularFile(managedGoplsPath())
}

// goplsInstallCommand is how gopls is installed without See, shown in
// Settings beside See's own installation.
func goplsInstallCommand() string {
	return "go install golang.org/x/tools/gopls@" + goplsVersion
}

// installGopls builds gopls into a stage with the Go toolchain, which is
// what ships gopls: installing Go alone does not bring it.
func installGopls(ctx context.Context, dest string) error {
	if err := shellpath.Wait(ctx); err != nil {
		return err
	}
	if dest == "" {
		return errors.New("cannot locate the application data folder")
	}
	if _, err := os.Lstat(dest); err == nil {
		return errors.New("a gopls installation already exists; remove it first")
	} else if !os.IsNotExist(err) {
		return err
	}
	gocmd, err := exec.LookPath("go")
	if err != nil {
		return errors.New("install Go first: gopls is built by the Go toolchain")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dest), ".gopls-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	bin := filepath.Join(stage, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, gocmd, "install", "golang.org/x/tools/gopls@"+goplsVersion)
	cmd.Env = append(os.Environ(), "GOBIN="+bin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installation failed: %s", lastLine(string(output)))
	}
	if !regularFile(filepath.Join(bin, toolExe("gopls"))) {
		return errors.New("installation did not contain gopls")
	}
	return os.Rename(stage, dest)
}

// lastLine is the last thing a toolchain said, which is where it says what
// went wrong.
func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return shortErr(errors.New(line))
		}
	}
	return "no output"
}

// ----- Shared -----

func regularFile(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// fetch reads url, which may be a release or its checksum.
func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %s", shortErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	b, err := readLimited(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download failed: %s", shortErr(err))
	}
	if int64(len(b)) > limit {
		return nil, errors.New("the download is larger than expected")
	}
	return b, nil
}

// readLimited reads r, refusing a member of an archive that is too big.
func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, downloadLimit+1))
	if err != nil {
		return nil, err
	}
	if len(b) > downloadLimit {
		return nil, errors.New("the download is larger than expected")
	}
	return b, nil
}

// verifyChecksum compares a download with the release's "<sha256>  <name>"
// line, so a truncated or replaced download is not installed.
func verifyChecksum(data, sum []byte) error {
	fields := strings.Fields(string(sum))
	if len(fields) == 0 {
		return errors.New("the release had no checksum")
	}
	got := sha256.Sum256(data)
	if !strings.EqualFold(fields[0], hex.EncodeToString(got[:])) {
		return errors.New("the download did not match its checksum")
	}
	return nil
}

// runInstallJob runs fn off the UI thread, marks the tool's row as working,
// and hands the result back on it.
func (a *App) runInstallJob(id string, services ui.Services, fn func(context.Context) error, done func()) {
	if a.serverJob {
		return
	}
	a.serverJob, a.serverJobName, a.serverJobErr = true, id, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
		defer cancel()
		err := fn(ctx)
		a.post.Post(func() {
			// The name stays, so the row its error belongs to can show it.
			a.serverJob = false
			if err != nil {
				a.serverJobErr = err.Error()
			}
			if done != nil {
				done()
			}
			services.Invalidate()
		})
	}()
}

// removeManagedTool deletes one of See's installations, keeping the folder
// when it cannot be deleted so its row stays honest.
func (a *App) removeManagedTool(id string, services ui.Services, dir string, done func()) {
	if a.serverJob {
		return
	}
	a.serverJob, a.serverJobName, a.serverJobErr = true, id, ""
	go func() {
		var err error
		if dir == "" {
			err = errors.New("cannot locate the application data folder")
		} else {
			err = os.RemoveAll(dir)
		}
		a.post.Post(func() {
			a.serverJob = false
			if err != nil {
				a.serverJobErr = err.Error()
			}
			if done != nil {
				done()
			}
			services.Invalidate()
		})
	}()
}

// ----- The tools in the app -----

func (a *App) installManagedRipgrep(services ui.Services) {
	a.runInstallJob("ripgrep", services, func(ctx context.Context) error {
		return installRipgrep(ctx, managedRipgrepDir())
	}, func() {
		// The next find, and the file list, use the new binary.
		a.find.rg, a.find.rgErr = "", nil
		a.quick.files, a.quick.truncated, a.quick.err = nil, false, nil
		if a.find.opts.Query != "" {
			a.findSoon()
		}
	})
}

func (a *App) removeManagedRipgrep(services ui.Services) {
	a.removeManagedTool("ripgrep", services, managedRipgrepDir(), func() {
		if a.find.cancel != nil {
			a.find.cancel()
		}
		a.find.rg, a.find.rgErr = "", nil
		a.find.err = nil
		a.quick.files, a.quick.truncated, a.quick.err = nil, false, nil
	})
}

func (a *App) installManagedGopls(services ui.Services) {
	a.runInstallJob("gopls", services, func(ctx context.Context) error {
		return installGopls(ctx, managedGoplsDir())
	}, func() {
		if a.serverJobErr == "" {
			a.retryLSP()
		}
	})
}

func (a *App) removeManagedGopls(services ui.Services) {
	a.removeManagedTool("gopls", services, managedGoplsDir(), func() {
		if a.lsp != nil {
			a.lsp.StopWait(2 * time.Second)
		}
		a.lspState, a.lspErr = langserver.Off, nil
	})
}

// findGopls finds the gopls See runs: $SEE_GOPLS or one on PATH, or else
// the copy See installed.
func findGopls(root string) (string, error) {
	return languages.FindGopls(root, managedGoplsPath())
}
