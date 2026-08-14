#!/usr/bin/env bash
# Scans the two tool modules (tools/ and tools/serve) for vulnerabilities.
#
# 'make deps-vulncheck' is 'govulncheck ./...' from the repository root, which
# covers the main module only. This repository has three Go modules, and the
# other two held reachable advisories nobody was looking at -- including
# GO-2026-5970 in x/text, the same advisory PR #119 had already fixed in the
# main module. One module scanned out of three is not a scan.
#
# Source-mode scanning cannot reach these modules at all. Each is a build-tagged
# stub whose only content is a blank import of a command, which keeps the tool's
# version pinned in go.mod (without it, 'go mod tidy' drops the tool entirely).
# So every source-mode invocation fails, and these are dead ends -- do not retry
# them:
#
#   govulncheck ./...              -> no packages matched the provided patterns
#   govulncheck -tags generate ./... -> "is a program, not an importable package"
#   govulncheck -scan module       -> build constraints exclude all Go files
#   govulncheck -scan module ./... -> patterns are not accepted for module scanning
#
# Binary mode is the answer, and not merely as a workaround: a module whose whole
# purpose is producing executables is most honestly scanned as the executables it
# produces. It also scans exactly what runs -- only linked code is present.
#
# Findings are classified the way govulncheck itself does. A finding whose first
# trace frame names a function is one the tool actually calls; anything else is
# present in the module graph but not invoked. Only the former fails this script.
# That is the same standard 'make deps-vulncheck' applies to the main module, so
# the two targets mean the same thing by "vulnerable".
#
# Exits 0 when no reachable vulnerability is found outside PERMANENT_OSV, 1
# otherwise. Unreachable findings are always printed and never fail the run.

set -euo pipefail

GO="${GO:-go}"
GOVULNCHECK_VERSION="${GOVULNCHECK_VERSION:-v1.6.0}"
GOVULNCHECK="golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"

# Advisories with no fix in any release, so no bump can clear them. Keeping this
# list explicit is what lets the check fail loudly on everything else; an
# unfiltered scan would be permanently red and stop being read. Each entry needs
# a reason and, if one exists, the condition under which it can be removed.
#
#   GO-2026-5932  golang.org/x/crypto/openpgp is unmaintained and unsafe by
#                 design. "Fixed in: N/A" -- upstream will not patch it. Reached
#                 by tfupdate for release-artifact signature verification, which
#                 runs on our own inputs at development time. Removable only if
#                 tfupdate drops the dependency.
PERMANENT_OSV=(
  GO-2026-5932
)

# module directory -> the command it exists to build. tools/serve pins its own
# copy of tfplugindocs, and its versions drifted furthest behind, so it is
# scanned separately rather than assumed to match tools/.
TARGETS=(
  "tools:github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
  "tools:github.com/minamijoyo/tfupdate"
  "tools/serve:github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for cmd in jq "$GO"; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "error: $cmd is required but not on PATH" >&2
    exit 1
  fi
done

# An explicit template rather than a bare 'mktemp -d': macOS mktemp ignores a -p
# flag and some sandboxes deny the default /var/folders location.
WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/vulncheck-tools.XXXXXX")"
trap 'rm -rf "$WORKDIR"' EXIT

permanent_osv_pattern=""
for osv in "${PERMANENT_OSV[@]}"; do
  permanent_osv_pattern="${permanent_osv_pattern}${permanent_osv_pattern:+|}${osv}"
done

failed=0

for target in "${TARGETS[@]}"; do
  module="${target%%:*}"
  pkg="${target#*:}"
  label="${module} -> $(basename "$pkg")"
  binary="${WORKDIR}/$(echo "${module}-$(basename "$pkg")" | tr '/' '-')"

  echo "==> ${label}"

  if ! (cd "${REPO_ROOT}/${module}" && "$GO" build -o "$binary" "$pkg"); then
    echo "    ERROR: build failed; cannot scan ${label}" >&2
    failed=1
    continue
  fi

  # govulncheck exits 3 when it finds anything, so the exit code cannot
  # distinguish "reachable" from "present in the graph" -- the JSON can.
  scan="${binary}.json"
  set +e
  (cd "${REPO_ROOT}/${module}" && "$GO" run "$GOVULNCHECK" \
    -format json -mode=binary "$binary") >"$scan" 2>"${scan}.err"
  scan_status=$?
  set -e

  if [ ! -s "$scan" ]; then
    echo "    ERROR: govulncheck produced no output (exit ${scan_status})" >&2
    sed 's/^/    /' "${scan}.err" >&2
    failed=1
    continue
  fi

  # A finding whose first trace frame names a function is one the binary calls.
  reachable="$(jq -sr '[.[] | select(.finding) | .finding
                        | select(.trace[0].function != null) | .osv]
                       | unique | .[]' "$scan")"
  unreachable="$(jq -sr '[.[] | select(.finding) | .finding
                          | select(.trace[0].function == null) | .osv]
                         | unique | .[]' "$scan")"

  # Report unreachable findings without failing: they are real advisories in the
  # graph, and a bump that clears one is worth taking, but nothing calls them.
  for osv in $unreachable; do
    case "$reachable" in
      *"$osv"*) continue ;;  # also reachable; reported below instead
    esac
    echo "    (not called) ${osv}  https://pkg.go.dev/vuln/${osv}"
  done

  for osv in $reachable; do
    if [ -n "$permanent_osv_pattern" ] && [[ "$osv" =~ ^(${permanent_osv_pattern})$ ]]; then
      echo "    (accepted)   ${osv}  no fix in any release; see PERMANENT_OSV"
      continue
    fi
    echo "    CALLED       ${osv}  https://pkg.go.dev/vuln/${osv}" >&2
    failed=1
  done
done

if [ "$failed" -ne 0 ]; then
  echo
  echo "Reachable vulnerabilities found in the tool modules." >&2
  echo "Fix by bumping the module inside tools/ or tools/serve, e.g." >&2
  echo "  cd tools && go get golang.org/x/text@latest && go mod tidy" >&2
  echo "Re-run 'make deps-vulncheck-tools' to confirm." >&2
  exit 1
fi

echo
echo "No reachable vulnerabilities in the tool modules."
