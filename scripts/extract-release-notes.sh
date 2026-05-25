#!/usr/bin/env bash
# Extracts the current version's section from CHANGELOG.md into .release-notes.md.
# Called by GoReleaser before.hooks with {{ .Version }} as $1 (bare version,
# e.g. "0.2.0"). GoReleaser resolves .Version from the git tag before invoking
# the hook, so it correctly handles both normal releases and GORELEASER_CURRENT_TAG
# overrides without the script needing to know about either.
#
# The extract strips the heading line (the GitHub Release UI shows the version in
# its own title bar, so duplicating it in the body is noise).  It also stops before
# the reference-link definitions block at the bottom of the file (lines starting
# with bare "["), so no dangling bracket references appear in the release body.
set -euo pipefail

VERSION="${1:?Version argument required (e.g. 0.2.0)}"

rm -f .release-notes.md

awk -v v="$VERSION" '
  /^## \[/ { if (p) exit; if ($0 ~ "^## \\[" v "\\]") { p=1; next } }
  /^\[/    { if (p) exit }
  p
' CHANGELOG.md > .release-notes.md

if [ ! -s .release-notes.md ]; then
    echo "ERROR: .release-notes.md is empty - no CHANGELOG section found for ${VERSION}; update CHANGELOG.md before tagging" >&2
    exit 1
fi
