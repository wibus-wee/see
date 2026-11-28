package app

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"

	"see/internal/localhistory"
	"see/internal/workspace"
	"see/locales"
)

const (
	localHistoryDebounce   = 1500 * time.Millisecond
	localHistoryCheckpoint = 30 * time.Second
)

func localHistoryText(key string) string {
	return locales.Catalog.Format(i18n.Locale(), key, nil)
}

func localHistoryFormat(key string, values map[string]any) string {
	return locales.Catalog.Format(i18n.Locale(), key, values)
}

func (a *App) localHistoryDocumentEligible(d *doc) bool {
	return d != nil && !d.untitled && !d.lossy && !d.video && d.image == nil && d.kind == workspace.Text && d.buf != nil
}

func (a *App) localHistorySupported(d *doc) bool {
	if !a.localHistoryDocumentEligible(d) {
		return false
	}
	return localHistoryTextSupported(d.buf.String())
}

func localHistoryTextSupported(text string) bool {
	return len(text) <= localhistory.MaxTextBytes && utf8.ValidString(text) && !strings.ContainsRune(text, '\x00')
}

func (a *App) captureLoadedLocalHistory(d *doc, diskText string) {
	if d == nil || d.buf == nil {
		return
	}
	now := time.Now()
	current := d.buf.String()
	if a.localHistoryDocumentEligible(d) {
		if localHistoryTextSupported(diskText) {
			a.writeLocalHistory(d, diskText, now)
		}
		if current != diskText && localHistoryTextSupported(current) {
			a.writeLocalHistory(d, current, now.Add(time.Nanosecond))
		}
	}
	d.localHistoryVer = d.buf.Version()
	d.localHistorySaved = now
}

func (a *App) captureEditedLocalHistory(d *doc) {
	if d == nil || d.buf == nil {
		return
	}
	version := d.buf.Version()
	if version == d.localHistoryVer {
		return
	}
	d.localHistoryVer = version
	if !a.localHistoryDocumentEligible(d) {
		return
	}
	snapshot := d.buf.Snapshot()
	text := snapshot.String()
	if !localHistoryTextSupported(text) {
		return
	}
	now := time.Now()
	if d.localHistorySaved.IsZero() || now.Sub(d.localHistorySaved) >= localHistoryCheckpoint {
		if d.localHistoryTimer != nil {
			d.localHistoryTimer.Stop()
			d.localHistoryTimer = nil
		}
		d.localHistorySaved = now
		a.writeLocalHistory(d, text, now)
		return
	}
	if d.localHistoryTimer != nil {
		d.localHistoryTimer.Stop()
	}
	path := d.path
	d.localHistoryTimer = time.AfterFunc(localHistoryDebounce, func() {
		a.post.Post(func() {
			if a.docs[path] != d || d.buf == nil || d.path != path || d.localHistoryVer != version || d.buf.Version() != version {
				return
			}
			d.localHistoryTimer = nil
			d.localHistorySaved = time.Now()
			a.writeLocalHistory(d, text, now)
		})
	})
}

func (a *App) captureCurrentLocalHistory(d *doc) {
	if d == nil || d.buf == nil {
		return
	}
	if d.localHistoryTimer != nil {
		d.localHistoryTimer.Stop()
		d.localHistoryTimer = nil
	}
	d.localHistoryVer = d.buf.Version()
	d.localHistorySaved = time.Now()
	if !a.localHistoryDocumentEligible(d) {
		return
	}
	text := d.buf.Snapshot().String()
	if localHistoryTextSupported(text) {
		a.writeLocalHistory(d, text, d.localHistorySaved)
	}
}

func (a *App) writeLocalHistory(d *doc, text string, at time.Time) {
	if a.localHistoryStore == nil || d == nil || !a.localHistoryDocumentEligible(d) || !localHistoryTextSupported(text) {
		return
	}
	store, path, post := a.localHistoryStore, d.path, a.post.Post
	a.localHistoryWrites.Add(1)
	go func() {
		defer a.localHistoryWrites.Done()
		if _, _, err := store.Add(path, text, at); err != nil {
			post(func() { a.fail(localHistoryText("localHistoryWriteError") + err.Error()) })
		}
	}()
}
