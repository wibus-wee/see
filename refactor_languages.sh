#!/bin/sh
set -e
cd /data/see-refactor

mkdir -p internal/languages
git mv internal/app/language_servers.go internal/languages/languages.go

# the TypeScript installer: the app methods stay, the rest moves
head -99 internal/app/server_install.go > internal/languages/install.go
tail -n +100 internal/app/server_install.go > internal/app/install.go
rm internal/app/server_install.go

# packages
sed -i 's/^package app$/package languages/' internal/languages/*.go

# the moved identifiers are the package's API now
sed -i -E 's/\berrNoGopls\b/ErrNoGopls/g; s/\bfindGopls\b/FindGopls/g; s/\bstartGopls\b/StartGopls/g;
 s/\berrNoTypeScriptServer\b/ErrNoTypeScriptServer/g; s/\bnewTypeScriptServer\b/NewTypeScriptServer/g;
 s/\btypeScriptLanguage\b/TypeScriptLanguage/g; s/\bfindTypeScriptServer\b/FindTypeScript/g;
 s/\bstartTypeScriptServer\b/StartTypeScript/g; s/\bmanagedTypeScriptDir\b/ManagedTypeScriptDir/g;
 s/\bmanagedTypeScriptEntry\b/ManagedTypeScriptEntry/g; s/\bmanagedTypeScriptExists\b/ManagedTypeScriptExists/g;
 s/\bmanagedTypeScriptInstalled\b/ManagedTypeScriptInstalled/g; s/\binstallTypeScriptServer\b/InstallTypeScript/g' internal/languages/*.go

# the app calls them through the package
for f in internal/app/*.go; do
  sed -i -E 's/\bErrNoGopls\b/languages.ErrNoGopls/g; s/\bFindGopls\(/languages.FindGopls(/g; s/\bStartGopls\(/languages.StartGopls(/g;
   s/\bErrNoTypeScriptServer\b/languages.ErrNoTypeScriptServer/g; s/\bNewTypeScriptServer\(/languages.NewTypeScriptServer(/g;
   s/\bTypeScriptLanguage\(/languages.TypeScriptLanguage(/g; s/\bFindTypeScriptServer\(/languages.FindTypeScript(/g;
   s/\bStartTypeScriptServer\(/languages.StartTypeScript(/g; s/\bmanagedTypeScriptDir\(/languages.ManagedTypeScriptDir(/g;
   s/\bmanagedTypeScriptEntry\(/languages.ManagedTypeScriptEntry(/g; s/\bmanagedTypeScriptExists\(/languages.ManagedTypeScriptExists(/g;
   s/\bmanagedTypeScriptInstalled\(/languages.ManagedTypeScriptInstalled(/g; s/\binstallTypeScriptServer\(/languages.InstallTypeScript(/g' "$f"
done

# files that now name languages need its import
for f in internal/app/*.go; do
  if grep -q 'languages\.' "$f" && ! grep -q '"see/internal/languages"' "$f"; then
    sed -i 's|^\t"see/internal/lsp"$|\t"see/internal/langserver"\n\t"see/internal/languages"|' "$f"
    if ! grep -q '"see/internal/languages"' "$f"; then
      sed -i 's|^import ($|import (\n\t"see/internal/languages"|' "$f"
    fi
  fi
done

gofmt -w internal/languages internal/app 2>&1 | head
go build ./... 2>&1 | head -30
echo BUILD_DONE