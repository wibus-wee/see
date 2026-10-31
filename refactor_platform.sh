#!/bin/sh
set -e
cd /data/see-refactor

git rm -q internal/app/host.go

# the ports
sed -i -E 's/^(\thost[[:space:]]+)host$/\1platform.Desktop/; s/^(\tpost[[:space:]]+)func\(func\(\)\)/\1platform.Scheduler/' internal/app/app.go

for f in internal/app/*.go; do
  # call sites
  for m in setTitle setWorkspaceSize popupMenu pickFolder pickNewFile confirmDirty trashItem copyFiles copyImage openProjectWindow refreshMenu setAppearance openSettings closeSettings refreshSettings openFontPopover closeFontPopover pickTerminalApp loadTerminalIcon; do
    M="$(printf '%s' "$m" | sed -E 's/^([a-z])/\U\1/')"
    sed -i -E "s/\\.$m\\(/.$M(/g" "$f"
    sed -i -E "s/func \\(h \\*windowHost\\) $m\\(/func (h *windowHost) $M(/g" "$f"
    sed -i -E "s/func \\(f \\*fakeHost\\) $m\\(/func (f *fakeHost) $M(/g" "$f"
  done
  # the UI thread
  sed -i -E 's/\ba\.post\(/a.post.Post(/g' "$f"
  sed -i -E 's/\bnoHost\b/platform.NoDesktop/g' "$f"
  sed -i -E 's/\berrNoDialog\b/platform.ErrNoDialog/g' "$f"
done

gofmt -w internal/app internal/platform 2>&1 | head -3
go build ./... 2>&1 | head -30
echo BUILD_DONE