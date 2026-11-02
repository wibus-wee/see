package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/avatar"
	"see/internal/git"
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

// personAvatar is the picture of someone, size DIPs across, as Peek's:
// the picture cut to a circle, else the initials Avatar draws for its
// own size, scaled to this one.
func (a *App) personAvatar(c *ui.Context, name, email string, size float32) ui.Element {
	box := ui.Box(c.Key("avatar:"+email)).Size(size, size).Shrink(0).Radius(size / 2).Role(ui.RoleImage).Label(name)
	if bm := a.avatarOf(email); bm != nil {
		return box.Clip().Children(func() {
			ui.Image(c, bm).Fit(ui.Cover).Size(size, size)
		})
	}
	own := c.Theme().FontSize * 2.25
	k, off := size/own, (size-own)/2
	return box.Children(func() { ui.Avatar(c, name, nil).Absolute().Left(off).Top(off).Scale(k, k) })
}

// commitAvatars is a commit's author's picture with its first
// co-author's over the corner, a ring of the background's color between
// them, as GitHub stacks them.
func (a *App) commitAvatars(c *ui.Context, cm git.Commit, size float32) {
	ui.Box(c).Size(size, size).Shrink(0).Tooltip(authors(cm)).Children(func() {
		a.personAvatar(c, cm.Author, cm.Email, size)
		if len(cm.CoAuthors) > 0 {
			co := cm.CoAuthors[0]
			small := size * 0.53
			ring := small + 3
			ui.Box(c).Size(ring, ring).Radius(ring / 2).Center().Background(colors(c).Sidebar).
				Absolute().Right(-4).Bottom(-4).Children(func() {
				a.personAvatar(c, co.Name, co.Email, small)
			})
		}
	})
}

// personLine is someone of a commit in its tab: their picture, their
// name with their part where they are not the author, and under it detail.
func (a *App) personLine(c *ui.Context, p git.Person, role, detail string) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		a.personAvatar(c, p.Name, p.Email, 28)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.Name).FontSize(13).Bold().SingleLine()
				if role != "" {
					ui.Text(c, role).FontSize(11).TextColor(colors(c).Muted).Background(colors(c).Pill).Padding(0, 5).Radius(4).SingleLine()
				} else {
					ui.Text(c, p.Email).FontSize(12).TextColor(colors(c).Muted).SingleLine().Shrink(1)
				}
			})
			ui.Text(c, detail).FontSize(12).TextColor(colors(c).Muted).SingleLine()
		})
	})
}
