#!/bin/sh
# Cut a release on macOS/Linux. With no arg, auto-increments the patch number in
# VERSION; with an arg, uses that exact version. Writes VERSION, commits,
# pushes, then tags and pushes -- the tag push triggers the GitHub workflow that
# builds and publishes every platform.
#
# Usage: release.sh [X.Y.Z]
set -e

NV="$1"
if [ -z "$NV" ]; then
  CUR="$(tr -d ' \r\n' < VERSION)"
  NV="${CUR%.*}.$(( ${CUR##*.} + 1 ))"
fi

printf '%s\n' "$NV" > VERSION
git add VERSION
git commit -m "chore: release v$NV"
git push origin HEAD
git tag -a "v$NV" -m "Release v$NV"
git push origin "v$NV"
echo "Released v$NV. GitHub will build and publish all platforms."
