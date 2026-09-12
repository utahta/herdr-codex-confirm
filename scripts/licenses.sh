#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
go mod download all
go list -m -f '{{if not .Main}}{{.Path}} {{.Dir}}{{end}}' all > "$temp/modules"
while read -r module directory; do
	[ -n "$directory" ] || continue
	for name in LICENSE LICENSE.md LICENSE.txt COPYING; do
		if [ -f "$directory/$name" ]; then
			printf '\n%s\n\n' "$module"
			cat "$directory/$name"
			break
		fi
	done
done < "$temp/modules" > "$temp/THIRD_PARTY_NOTICES"
mv "$temp/THIRD_PARTY_NOTICES" THIRD_PARTY_NOTICES
