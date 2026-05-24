#!/usr/bin/env bash
# Extracts the current version's section from CHANGELOG.md into .release-notes.md.
# Called by GoReleaser before.hooks; GORELEASER_CURRENT_TAG is set by GoReleaser.
#
# The extract strips the heading line (the GitHub Release UI shows the version in
# its own title bar, so duplicating it in the body is noise).  It also stops before
# the reference-link definitions block at the bottom of the file (lines starting
# with bare "["), so no dangling bracket references appear in the release body.
set -euo pipefail

rm -f .release-notes.md

VERSION="${GORELEASER_CURRENT_TAG#v}"

awk -v v="$VERSION" '
  /^## \[/ { if (p) exit; if ($0 ~ "^## \\[" v "\\]") { p=1; next } }
  /^\[/    { if (p) exit }
  p
' CHANGELOG.md > .release-notes.md

if [ ! -s .release-notes.md ]; then
    echo "ERROR: .release-notes.md is empty - no CHANGELOG section found for ${GORELEASER_CURRENT_TAG}; update CHANGELOG.md before tagging" >&2
    exit 1
fi
