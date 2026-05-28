# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
#
# Verify every datahub provider version pin in examples/ matches the version
# being released. Called by .goreleaser.yml before.hooks before any artifact
# is built, so a mismatch aborts the release cleanly.
#
# Usage: check-example-versions.sh <version>
# Example: check-example-versions.sh 0.3.0

set -euo pipefail

VERSION="${1:?Usage: $0 <version>}"
FAILED=0
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

while IFS= read -r -d '' tf_file; do
  # Match the quoted source value to avoid false positives from URLs that
  # happen to contain "datahub-project/datahub" as a path component.
  if ! grep -qE 'source[[:space:]]*=[[:space:]]*"[^"]*datahub-project/datahub[^"]*"' "$tf_file"; then
    continue
  fi

  # Extract the version from the datahub provider block.
  # The canonical form is:
  #   datahub = {
  #     source  = "datahub-project/datahub"
  #     version = "X.Y.Z"
  #   }
  # We capture the version value on the line following the source line.
  pinned=$(awk '
    /source[[:space:]]*=[[:space:]]*"[^"]*datahub-project\/datahub[^"]*"/ { in_block=1; next }
    in_block && /version[[:space:]]*=/ {
      line = $0
      sub(/^[^"]*"/, "", line)
      sub(/".*$/, "", line)
      print line
      in_block=0
    }
    in_block && /}/ { in_block=0 }
  ' "$tf_file")

  if [ -z "$pinned" ]; then
    printf 'MISSING version pin in: %s\n' "$tf_file" >&2
    FAILED=1
  elif [ "$pinned" != "$VERSION" ]; then
    printf 'MISMATCH: %s: found "%s", want "%s"\n' "$tf_file" "$pinned" "$VERSION" >&2
    FAILED=1
  fi
done < <(find "$REPO_ROOT/examples" -name "*.tf" -print0)

if [ "$FAILED" -ne 0 ]; then
  printf '\nFix with: make bump-examples VERSION=%s && make generate\n' "$VERSION" >&2
  exit 1
fi
