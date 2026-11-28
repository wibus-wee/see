package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"see/locales"

	"see/internal/workspace"
)

type recoveryRecord struct {
	Root, Path, Base, Text string
	BOM                    bool
}

func recoveryPath(root, path string) (string, error) {
	dir, err := draftDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(root + "\x00" + path))
	return filepath.Join(dir, fmt.Sprintf("recovery-%x.json", sum)), nil
}

func readRecovery(root, path string) *recoveryRecord {
	name, err := recoveryPath(root, path)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil
	}
	var r recoveryRecord
	if json.Unmarshal(data, &r) != nil || r.Root != root || r.Path != path {
		return nil
	}
	return &r
}

// pendingStandaloneRecovery filters a saved-file recovery before restoring its
// tab. A process may die after writing the file but before deleting the record;
// if the record already equals disk there is no unsaved work to reopen.
func pendingStandaloneRecovery(path string) *recoveryRecord {
	r := readRecovery("", path)
	if r == nil {
		return nil
	}
	f, err := workspace.ReadFile(path)
	if err == nil && f.Kind == workspace.Text && r.Text == f.Text {
		if name, pathErr := recoveryPath("", path); pathErr == nil {
			_ = os.Remove(name)
		}
		return nil
	}
	if err == nil && f.Kind != workspace.Text || err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return r
}

func (a *App) cacheRecovery(d *doc) {
	if !d.buf.Modified() {
		a.removeRecovery(d)
		return
	}
	if d.cache == nil {
		d.cache = &draftCache{}
	}
	cache := d.cache
	name, err := recoveryPath(a.root, d.path)
	if err != nil {
		a.fail(locales.MsgCouldNotCacheUnsavedFile() + err.Error())
		return
	}
	snap := d.buf.Snapshot()
	record := recoveryRecord{Root: a.root, Path: d.path, Base: d.diskText, Text: snap.String(), BOM: d.bom}
	post := a.post.Post
	go func() {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if cache.removed || snap.Version() < cache.version {
			return
		}
		data, err := json.Marshal(record)
		if err == nil {
			err = os.MkdirAll(filepath.Dir(name), 0700)
		}
		var tmp *os.File
		if err == nil {
			tmp, err = os.CreateTemp(filepath.Dir(name), ".recovery-")
		}
		if err == nil {
			defer os.Remove(tmp.Name())
			_, err = tmp.Write(data)
			if e := tmp.Close(); err == nil {
				err = e
			}
			if err == nil {
				err = os.Rename(tmp.Name(), name)
			}
		}
		if err == nil {
			cache.version = snap.Version()
			post(func() {})
		} else {
			post(func() { a.fail(locales.MsgCouldNotCacheUnsavedFile() + err.Error()) })
		}
	}()
}

func (a *App) removeRecovery(d *doc) {
	name, err := recoveryPath(a.root, d.path)
	if err != nil {
		return
	}
	cache := d.cache
	if cache == nil {
		cache = &draftCache{}
	}
	d.cache = nil
	// Serialize removal with pending writes, invalidating them before returning.
	cache.mu.Lock()
	cache.removed = true
	_ = os.Remove(name)
	cache.mu.Unlock()
}
