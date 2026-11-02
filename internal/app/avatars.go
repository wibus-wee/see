package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/avatar"
)

// Authors' pictures, read off the UI thread once per address and kept
// for the app's life; initials show until one comes, and where there is
// none.

type avatarEntry struct {
	bitmap  *ui.Bitmap
	loading bool
}

var (
	avatarClient = &http.Client{Timeout: 15 * time.Second}
	avatarSlots  = make(chan struct{}, 4) // downloads at a time
)

// avatarFetch is avatar.Fetch in the app; tests take the network away.
var avatarFetch = avatar.Fetch

// avatarOf is the picture of email, or nil while it is read or when
// there is none.
func (a *App) avatarOf(email string) *ui.Bitmap {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" {
		return nil
	}
	if a.avatars == nil {
		a.avatars = map[string]*avatarEntry{}
	}
	if e := a.avatars[key]; e != nil {
		return e.bitmap
	}
	e := &avatarEntry{loading: true}
	a.avatars[key] = e
	urls := avatar.URLs(key, hostName(a.git.web) == "GitHub", 128)
	go func() {
		avatarSlots <- struct{}{}
		defer func() { <-avatarSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		data, err := avatarFetch(ctx, avatarClient, avatar.CacheDir(), urls)
		var bmp *ui.Bitmap
		if err == nil {
			bmp, _ = ui.DecodeBitmap(data)
		}
		a.post.Post(func() {
			e.loading, e.bitmap = false, bmp
		})
	}()
	return nil
}

// personAvatar is a person's picture, or their initials, fontSize*2 high.
func (a *App) personAvatar(c *ui.Context, name, email string, fontSize float32) ui.Element {
	return ui.Avatar(c.Key("avatar:"+email), name, a.avatarOf(email)).FontSize(fontSize).Shrink(0).Tooltip(name + " <" + email + ">")
}
