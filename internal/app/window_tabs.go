package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"

	"see/locales"
)

func projectWindowMenu(isDarwin, canMerge bool, merge func()) *mygo.MenuItem {
	if !isDarwin {
		return &mygo.MenuItem{Role: mygo.RoleWindowMenu}
	}

	return &mygo.MenuItem{
		Role: mygo.RoleWindowMenu,
		Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize},
			{Role: mygo.RoleZoom},
			mygo.Separator(),
			{
				Label:    locales.Catalog.Format(i18n.Locale(), "msgMergeAllWindows", nil),
				Disabled: !canMerge,
				Click: func(*mygo.MenuItem, *mygo.Window) {
					if canMerge && merge != nil {
						merge()
					}
				},
			},
			{Role: mygo.RoleFront},
		},
	}
}
