#!/bin/sh
# Install each named npm project's dependencies when its lockfile is newer than
# the installed tree, then exec the command. A directory without a lockfile is
# skipped.
#
# Usage: entrypoint.sh DIR... -- CMD...
set -e

while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do
	dir="$1"
	shift
	[ -f "$dir/package-lock.json" ] || continue
	installed="$dir/node_modules/.package-lock.json"
	if [ ! -f "$installed" ] || [ "$dir/package-lock.json" -nt "$installed" ]; then
		(cd "$dir" && npm ci)
	fi
done
[ "$1" = "--" ] && shift

exec "$@"
