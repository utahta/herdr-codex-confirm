#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$temp/tools" "$temp/plugin/release" "$temp/assets"
cp install.sh herdr-plugin.toml "$temp/plugin/"
set -- dist/herdr-codex-confirm_v*.tar.gz
test "$#" -eq 4
for archive do
	tar -tzf "$archive" > "$temp/contents"
	for member in herdr-codex-confirm LICENSE THIRD_PARTY_NOTICES; do
		grep -Fx "$member" "$temp/contents" >/dev/null
	done
	tar -xOzf "$archive" THIRD_PARTY_NOTICES > "$temp/notices"
	test -s "$temp/notices"
done

reset_assets() {
	cp dist/*.tar.gz dist/checksums.txt dist/checksums.txt.sig "$temp/assets/"
	cp dist/signing-key.pub "$temp/plugin/release/signing-key.pub"
	: > "$temp/downloads"
}
reset_assets

# Restrict PATH so the test exercises downloads without invoking Go or a network.
for command in sh dirname sed uname mktemp rm awk wc tr tar gzip chmod mv cp cat ssh-keygen; do
	ln -s "$(command -v "$command")" "$temp/tools/$command"
done
if command -v sha256sum >/dev/null 2>&1; then
	ln -s "$(command -v sha256sum)" "$temp/tools/sha256sum"
else
	ln -s "$(command -v shasum)" "$temp/tools/shasum"
fi
cat > "$temp/tools/curl" <<'SH'
#!/bin/sh
set -eu
asset=
output=
while [ "$#" -gt 0 ]; do
  case "$1" in
    https://github.com/utahta/herdr-codex-confirm/releases/download/v*) asset=${1##*/};;
    -o) shift; output=$1;;
  esac
  shift
done
test -n "$asset"
test -n "$output"
printf '%s\n' "$asset" >> "$CONFIRM_TEST_DOWNLOAD_LOG"
cp "$CONFIRM_TEST_ASSETS/$asset" "$output"
SH
chmod +x "$temp/tools/curl"
run_install() {
	PATH="$temp/tools" CONFIRM_TEST_ASSETS="$temp/assets" CONFIRM_TEST_DOWNLOAD_LOG="$temp/downloads" /bin/sh "$temp/plugin/install.sh"
}
reject_install() {
	if run_install > "$temp/failure" 2>&1; then
		echo "Installer accepted $1." >&2
		exit 1
	fi
	grep -F "$2" "$temp/failure" >/dev/null || { cat "$temp/failure" >&2; exit 1; }
	test ! -e "$temp/plugin/herdr-codex-confirm"
	echo "Rejected $1."
}
assert_no_archive_download() {
	if grep -q '\.tar\.gz$' "$temp/downloads"; then
		echo 'Downloaded an archive before validating signed checksums.' >&2
		exit 1
	fi
}

run_install
version=$(sed -n 's/^version = "\([0-9][0-9.]*\)"$/\1/p' herdr-plugin.toml)
test "$("$temp/plugin/herdr-codex-confirm" --version)" = "herdr-codex-confirm $version"
test -s "$temp/plugin/THIRD_PARTY_NOTICES"
rm "$temp/plugin/herdr-codex-confirm"

reset_assets
printf '# modified\n' >> "$temp/assets/checksums.txt"
reject_install 'modified checksums' 'Release signature verification failed.'
assert_no_archive_download

reset_assets
printf 'invalid signature\n' > "$temp/assets/checksums.txt.sig"
reject_install 'invalid signature' 'Release signature verification failed.'
assert_no_archive_download

reset_assets
rm "$temp/assets/checksums.txt.sig"
reject_install 'missing signature' 'checksums.txt.sig'
assert_no_archive_download

ssh-keygen -t ed25519 -N '' -C installer-test -f "$temp/test-key" -q
sign_checksums() {
	rm -f "$temp/assets/checksums.txt.sig"
	ssh-keygen -Y sign -f "$temp/test-key" -n "$1" "$temp/assets/checksums.txt" >/dev/null 2>&1
}

reset_assets
sign_checksums herdr-codex-confirm-release
reject_install 'signature from another key' 'Release signature verification failed.'
assert_no_archive_download

reset_assets
printf 'herdr-codex-confirm-release %s\n' "$(cat "$temp/test-key.pub")" > "$temp/plugin/release/signing-key.pub"
sign_checksums different-namespace
reject_install 'signature for another namespace' 'Release signature verification failed.'
assert_no_archive_download

reset_assets
printf 'herdr-codex-confirm-release %s\n' "$(cat "$temp/test-key.pub")" > "$temp/plugin/release/signing-key.pub"
sed 's/^[a-f0-9]/x/' "$temp/assets/checksums.txt" > "$temp/bad-checksums"
cp "$temp/bad-checksums" "$temp/assets/checksums.txt"
sign_checksums herdr-codex-confirm-release
reject_install 'signed malformed checksums' 'Release checksum is missing or ambiguous.'
assert_no_archive_download

reset_assets
for archive in "$temp/assets/"*.tar.gz; do
	printf '%s' 'corrupt' >> "$archive"
done
reject_install 'corrupted archive' 'Release checksum verification failed.'

reset_assets
rm "$temp/tools/ssh-keygen"
reject_install 'missing ssh-keygen' 'Install ssh-keygen'
test ! -s "$temp/downloads"
printf '#!/bin/sh\nexit 1\n' > "$temp/tools/ssh-keygen"
chmod +x "$temp/tools/ssh-keygen"
reject_install 'ssh-keygen without verification support' 'Release signature verification failed.'
assert_no_archive_download

printf '%s\n' 'Signed installer and artifact tests passed.'
