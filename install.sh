#!/bin/sh
set -eu

cd "$(dirname "$0")"
version=$(sed -n 's/^version = "\([0-9][0-9.]*\)"$/\1/p' herdr-plugin.toml)
case "$version" in ''|*[!0-9.]*) echo 'Cannot read release version.' >&2; exit 1;; esac

if command -v go >/dev/null 2>&1; then
	go build -trimpath -ldflags="-s -w -X main.version=$version" -o herdr-codex-confirm .
	exit 0
fi

case "$(uname -s)" in Darwin) os=darwin;; Linux) os=linux;; *) echo 'Supported systems: macOS and Linux.' >&2; exit 1;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=amd64;; *) echo 'Supported CPUs: arm64 and amd64.' >&2; exit 1;; esac
if ! command -v curl >/dev/null 2>&1; then echo 'Install Go 1.26+ or curl to install Codex Confirm.' >&2; exit 1; fi
if ! command -v ssh-keygen >/dev/null 2>&1; then echo 'Install ssh-keygen with -Y verify support to verify the release signature.' >&2; exit 1; fi
if command -v sha256sum >/dev/null 2>&1; then
	sumtool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
	sumtool='shasum -a 256'
else
	echo 'Install sha256sum or shasum to verify the release.' >&2; exit 1
fi

temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
asset="herdr-codex-confirm_v${version}_${os}_${arch}.tar.gz"
base="https://github.com/utahta/herdr-codex-confirm/releases/download/v$version"
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 10 --max-time 120 "$base/checksums.txt" -o "$temp/checksums.txt"
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 10 --max-time 120 "$base/checksums.txt.sig" -o "$temp/checksums.txt.sig"
if ! ssh-keygen -Y verify -f release/signing-key.pub -I herdr-codex-confirm-release -n herdr-codex-confirm-release \
	-s "$temp/checksums.txt.sig" < "$temp/checksums.txt"; then
	echo 'Release signature verification failed. A valid signature and ssh-keygen with -Y verify support are required.' >&2
	exit 1
fi
awk -v asset="$asset" 'NF == 2 && length($1) == 64 && $1 !~ /[^0-9a-f]/ && $2 == asset {print}' "$temp/checksums.txt" > "$temp/selected.txt"
if [ "$(wc -l < "$temp/selected.txt" | tr -d ' ')" != 1 ]; then echo 'Release checksum is missing or ambiguous.' >&2; exit 1; fi
curl -fsSL --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 10 --max-time 120 "$base/$asset" -o "$temp/$asset"
if ! (cd "$temp" && $sumtool -c selected.txt); then
	echo 'Release checksum verification failed.' >&2
	exit 1
fi
tar -xzf "$temp/$asset" -C "$temp" herdr-codex-confirm THIRD_PARTY_NOTICES
chmod +x "$temp/herdr-codex-confirm"
"$temp/herdr-codex-confirm" --version
mv "$temp/herdr-codex-confirm" ./herdr-codex-confirm
mv "$temp/THIRD_PARTY_NOTICES" ./THIRD_PARTY_NOTICES
