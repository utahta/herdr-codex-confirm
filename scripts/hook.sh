#!/bin/sh
set -u

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 2
if "$script_dir/../herdr-codex-confirm" hook "$@"; then
	exit 0
fi
echo 'Codex Confirm could not complete the hook; command blocked.' >&2
exit 2
