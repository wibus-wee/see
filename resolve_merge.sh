#!/bin/sh
set -e
cd /data/see-refactor

# 1. the tools file keeps their code, in the app package
sed -e 's/^package main$/package app/' -e 's/\*app\b/*App/g' /tmp/theirs-tools.go > internal/app/tools.go

# 2. settings keeps their page, with this branch's palette and languages API
sed -E \
  -e 's/^package main$/package app/' \
  -e 's/\*app\b/*App/g' \
  -e 's/colors\(c\)\.([a-z])([A-Za-z]*)/theme.Of(c).\U\1\E\2/g' \
  -e 's/\bdarkPalette\b/theme.Dark/g' \
  -e 's/\blightPalette\b/theme.Light/g' \
  -e 's/\bmanagedTypeScriptDir\(\)/languages.ManagedTypeScriptDir()/g' \
  -e 's/\bmanagedTypeScriptExists\(\)/languages.ManagedTypeScriptExists()/g' \
  -e 's/\bmanagedTypeScriptInstalled\(\)/languages.ManagedTypeScriptInstalled()/g' \
  -e 's/\bfindTypeScriptServer\(/languages.FindTypeScript(/g' \
  -e 's/\bfindGopls\(/languages.FindGopls(/g' \
  /tmp/theirs-settings.go > internal/app/settings.go

# palette field names in settings, which the sed above capitalised already
sed -i -E 's/theme\.(Dark|Light)\.([a-z])([A-Za-z]*)/theme.\1.\U\2\E\3/g' internal/app/settings.go
grep -q '"see/internal/theme"' internal/app/settings.go || sed -i 's|^\t"see/internal/langserver"$|\t"see/internal/langserver"\n\t"see/internal/theme"|' internal/app/settings.go

# 3. gopls found by the copy See installed: the same variadic seam as ripgrep
sed -i 's/^func FindGopls(root string) (string, error) {/func FindGopls(root string, installed ...string) (string, error) {/' internal/languages/languages.go
sed -i 's|^\tif p, err := exec.LookPath("gopls"); err == nil {|\tcandidates = append(candidates, installed...)\n\tif p, err := exec.LookPath("gopls"); err == nil {|' internal/languages/languages.go
sed -i 's|// FindGopls finds gopls: \$SEE_GOPLS, then PATH, then Go.s bin|// FindGopls finds gopls: $SEE_GOPLS, then the copies an installation added\n// (in the order given), then PATH, then Go s bin|' internal/languages/languages.go

gofmt -w internal/app internal/languages 2>&1 | head -3
go build ./... 2>&1 | head -25
echo BUILD_DONE