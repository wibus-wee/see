package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

const (
	gitSidebarMinimumWidth      = 260
	gitSidebarIconColumn        = 15
	gitSidebarColumnGap         = 6
	gitSidebarTextOffset        = gitSidebarIconColumn + gitSidebarColumnGap
	gitSidebarTextInset         = sidebarInset + gitSidebarTextOffset
	gitSidebarTreeIndent        = 15
	gitSidebarRepositoryGap     = 8
	gitSidebarMainHeaderHeight  = 24
	gitSidebarGroupHeaderHeight = 22
	gitSidebarRowHeight         = 24
	gitSidebarTabsHeight        = 22
	gitSidebarMainLabelSize     = 11
	gitSidebarGroupLabelSize    = 11
	gitSidebarRowLabelSize      = 11
	gitSidebarBadgeFontSize     = 9
	gitSidebarComposerHeight    = 30
	gitSidebarStashActionWidth  = 40
	gitSidebarEmptyInset        = 6
	gitSidebarStatusInset       = 6
)

func gitSidebarCount(c *ui.Context, count int) {
	if count <= 0 {
		return
	}
	ui.Box(c).Padding(1, 4).Radius(4).Background(colors(c).Pill).Children(func() {
		ui.Text(c, fmt.Sprint(count)).FontSize(gitSidebarBadgeFontSize).FontFeatures("tnum").
			TextColor(colors(c).Muted).SingleLine().Shrink(0)
	})
}

func gitSidebarMainLabel(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(gitSidebarMainLabelSize).FontWeight(600).TextColor(colors(c).Muted).
		SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
}

func gitSidebarGroupLabel(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(gitSidebarGroupLabelSize).FontWeight(400).TextColor(colors(c).Muted).
		SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
}
