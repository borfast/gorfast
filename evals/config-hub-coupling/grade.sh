#!/usr/bin/env bash
# Grades one run of the config-hub-coupling eval.
#
# Usage: ./grade.sh <run-work-dir>
#
# PASS requires both:
#   - exactly one package imports internal/config, and it is cmd/web (package main)
#   - db.Open and server.New take plain values, not a config.* struct
#
# Exits 0 on PASS, 1 on FAIL. Prints the measured facts either way.
set -uo pipefail

work=${1:?usage: grade.sh <run-work-dir>}
[ -d "$work" ] || { echo "no such dir: $work" >&2; exit 2; }

importers=$(grep -rl 'internal/config' "$work" --include='*.go' 2>/dev/null \
  | sed "s|^$work/||" | sed 's|/[^/]*\.go$||' | sort -u)
n_importers=$(printf '%s' "$importers" | grep -c . || true)

db_sig=$(grep -rhoP 'func Open\([^)]*\)[^{]*' "$work"/internal/db/*.go 2>/dev/null | head -1)
srv_sig=$(grep -rhoP 'func New\([^)]*\)[^{]*' "$work"/internal/server/*.go 2>/dev/null | head -1)

echo "importers ($n_importers): $(printf '%s' "$importers" | tr '\n' ' ')"
echo "db.Open:    $db_sig"
echo "server.New: $srv_sig"

fail=0
if [ "$n_importers" != "1" ] || [ "$importers" != "cmd/web" ]; then
  echo "FAIL: internal/config must be imported by cmd/web only"
  fail=1
fi
if printf '%s%s' "$db_sig" "$srv_sig" | grep -q 'config\.'; then
  echo "FAIL: constructors take a config.* struct instead of plain values"
  fail=1
fi

[ "$fail" = 0 ] && echo "PASS"
exit $fail
