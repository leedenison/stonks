#!/usr/bin/env bash
# Apply a SQL file to the dev database, or do nothing when no file is configured.
#
# Usage: scripts/seed-db.sh "<compose command>" [file]
set -e
compose="$1"
file="$2"

if [ -z "$file" ]; then
	echo "seed-db: skipped (DEV_SEED_SQL not set)"
	exit 0
fi
if [ ! -f "$file" ]; then
	echo "seed-db: skipped ($file does not exist)"
	exit 0
fi

$compose exec -T postgres psql -v ON_ERROR_STOP=1 -q -U stonks -d stonks <"$file"
echo "seed-db: applied $file"
