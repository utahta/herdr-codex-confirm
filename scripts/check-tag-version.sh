#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ "$#" -ne 1 ]; then
	echo 'Usage: check-tag-version.sh <tag>' >&2
	exit 2
fi
version=$(sed -n 's/^version = "\([0-9][0-9.]*\)"$/\1/p' herdr-plugin.toml)
if [ -z "$version" ] || [ "$1" != "v$version" ]; then
	echo "Tag $1 does not match manifest version $version." >&2
	exit 1
fi
