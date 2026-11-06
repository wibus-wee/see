#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

export SEE_RG="${SEE_RG:-$(command -v rg)}"
export SEE_GOPLS="${SEE_GOPLS:-$(command -v gopls)}"
test -x "$SEE_RG"
test -x "$SEE_GOPLS"
if [[ "${OS:-}" == "Windows_NT" ]]; then
  export SEE_RG="$(cygpath -m "$SEE_RG")"
  export SEE_GOPLS="$(cygpath -m "$SEE_GOPLS")"
fi

CGO_ENABLED=0 go test -count=1 -timeout=5m ./...
go tool mygo vet .

# Release-body fixtures execute a POSIX shell script directly.
if [[ "${OS:-}" != "Windows_NT" ]]; then
  python3 -m unittest discover -s scripts/changelog -p 'test_*.py'
fi
