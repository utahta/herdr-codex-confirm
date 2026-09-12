#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if ! command -v goreleaser >/dev/null 2>&1; then
	echo 'Install GoReleaser v2 to build release snapshots.' >&2
	exit 1
fi
RELEASE_VERSION=$(sed -n 's/^version = "\([0-9][0-9.]*\)"$/\1/p' herdr-plugin.toml)
sh scripts/check-tag-version.sh "v$RELEASE_VERSION"
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
ssh-keygen -t ed25519 -N '' -C snapshot -f "$temp/signing-key" -q
RELEASE_SIGNING_KEY_FILE="$temp/signing-key"
export RELEASE_VERSION RELEASE_SIGNING_KEY_FILE
goreleaser release --snapshot --clean
printf 'herdr-codex-confirm-release %s\n' "$(cat "$temp/signing-key.pub")" > dist/signing-key.pub
