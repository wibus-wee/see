#!/usr/bin/env bash
#
# release-body.sh writes a GitHub release body to stdout: first the direct
# download links for the release's installers, then the notes GitHub
# generated. The release page shows the assets in one undifferentiated
# list, deltas included, so the links go where people look first.
#
# Usage: release-body.sh <tag> <owner/repo>
# Needs gh, authenticated with a token that can read the release.
set -euo pipefail

tag="${1:?usage: release-body.sh <tag> <owner/repo>}"
repo="${2:?usage: release-body.sh <tag> <owner/repo>}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

gh release view "$tag" --repo "$repo" --json assets --jq '.assets[].name' | sort >"$work/assets"
gh release view "$tag" --repo "$repo" --json body --jq '.body' >"$work/notes"

# The body already written: leave it alone, so that a rerun of the release
# job does not stack two download sections.
if grep -q '^### Download' "$work/notes"; then
	cat "$work/notes"
	exit 0
fi

base="https://github.com/$repo/releases/download/$tag"

# asset <pattern> prints the first asset matching it, if any.
asset() { grep -E -- "$1" "$work/assets" | head -n1 || true; }

# link <pattern> <label> prints a Markdown link to the first matching asset.
link() {
	local file
	file="$(asset "$1")"
	[ -n "$file" ] || return 0
	printf '[%s](%s/%s)' "$2" "$base" "$file"
}

# row <heading> <links...> prints a line of the links it was given, if any.
row() {
	local heading="$1" out=()
	shift
	for item in "$@"; do
		[ -n "$item" ] && out+=("$item")
	done
	[ "${#out[@]}" -gt 0 ] || return 0
	local joined
	joined="$(printf '%s · ' "${out[@]}")"
	printf '**%s** — %s\n\n' "$heading" "${joined% · }"
}

{
	echo '### Download'
	echo
	row 'macOS' \
		"$(link '-arm64\.dmg$' 'Apple silicon')" \
		"$(link '-amd64\.dmg$' 'Intel')"
	row 'Windows' \
		"$(link 'setup-.*-amd64\.exe$' 'x64 installer')" \
		"$(link 'setup-.*-arm64\.exe$' 'arm64 installer')"
	row 'Linux' \
		"$(link '_amd64\.deb$' 'x64 .deb')" \
		"$(link '_arm64\.deb$' 'arm64 .deb')"
	portable=(
		"$(link 'darwin-arm64\.tar\.gz$' 'macOS arm64')"
		"$(link 'darwin-amd64\.tar\.gz$' 'macOS x64')"
		"$(link 'linux-arm64\.tar\.gz$' 'Linux arm64')"
		"$(link 'linux-amd64\.tar\.gz$' 'Linux x64')"
		"$(link 'windows-arm64\.tar\.gz$' 'Windows arm64')"
		"$(link 'windows-amd64\.tar\.gz$' 'Windows x64')"
	)
	kept=()
	for item in "${portable[@]}"; do
		[ -n "$item" ] && kept+=("$item")
	done
	if [ "${#kept[@]}" -gt 0 ]; then
		joined="$(printf '%s · ' "${kept[@]}")"
		printf '<sub>Portable archives: %s</sub>\n\n' "${joined% · }"
	fi
	echo '<sub>Installed apps update themselves; the delta files in the asset list are for that, not for installing.</sub>'
	echo
	echo '---'
	echo
	cat "$work/notes"
}