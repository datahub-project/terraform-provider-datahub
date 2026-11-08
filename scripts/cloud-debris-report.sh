#!/usr/bin/env bash
# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0
#
# Lists entities on a live DataHub instance that look like test debris: any
# entity whose URN, or whose aspect content, carries one of the identifier
# prefixes the acceptance tests and runnable examples use. READ-ONLY: it only
# issues GET requests and never deletes anything. Sweeping is a deliberate,
# separate act by a person.
#
# Used by .github/workflows/cloud-live.yml against the shared DataHub Cloud
# instance, once before the acceptance suite (a baseline) and once after (the
# report), so the summary can say which debris is new this run and which was
# already there. Usable by hand too:
#
#   DATAHUB_GMS_URL=... DATAHUB_GMS_TOKEN=... scripts/cloud-debris-report.sh --out debris.tsv
#
# Usage: cloud-debris-report.sh --out FILE [--baseline FILE] [--summary]
#
#   --out FILE       write one TSV line per hit: type, urn, match, state
#   --baseline FILE  an earlier --out file; hits absent from it are marked new
#   --summary        append a Markdown report to $GITHUB_STEP_SUMMARY (or
#                    stdout when unset) and emit a workflow warning on debris
#
# Exit status is 0 whether or not debris is found, and 0 when the instance
# cannot be reached (the failure is reported instead): this is a report, not a
# gate. Only a usage error or missing credentials exits non-zero.
#
# How it finds debris, and why each step is there:
#
# 1. Scroll every entity of each type the provider manages through the OpenAPI
#    v3 listing (GET /openapi/v3/entity/<type>), including soft-deleted ones
#    (a soft-deleted test entity still blocks re-creating its id) and with
#    skipCache, since DataHub Cloud answers a repeated wildcard search from a
#    result cache for minutes. Matching happens here, client-side, rather than
#    in the search query: the search index does not cover every id (a `test`
#    entity is not found by its id), and a hyphenated prefix is not reliably a
#    single search term.
# 2. Match the prefixes only at an identifier boundary (start of a JSON string,
#    or after ':' '(' ',' '/' or an encoded '%3A'). Unanchored, `RT_` would
#    match IMPORT_README, START_WITH and every REPORT_ in the instance.
# 3. Record whether the URN itself matched or only the aspect content did. A
#    content-only hit is usually a real entity that references a test entity
#    (a user still in a deleted test group), which is worth seeing but is not
#    itself debris to sweep.
# 4. Re-read every hit from the strongly consistent entity endpoint and drop
#    any that 404: the listing is search-backed, so an entity the suite deleted
#    seconds ago can still be listed.
#
# Only URNs, entity types and match kinds are ever printed. Aspect content is
# never printed: corpuser and secret aspects hold credential material.

set -uo pipefail

OUT=""
BASELINE=""
SUMMARY=0

usage() {
  sed -n '18,28p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="${2:-}"; shift 2 ;;
    --baseline) BASELINE="${2:-}"; shift 2 ;;
    --summary) SUMMARY=1; shift ;;
    -h|--help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
done

[ -n "$OUT" ] || usage

if [ -z "${DATAHUB_GMS_URL:-}" ] || [ -z "${DATAHUB_GMS_TOKEN:-}" ]; then
  echo "DATAHUB_GMS_URL and DATAHUB_GMS_TOKEN must be set." >&2
  exit 2
fi

GMS="${DATAHUB_GMS_URL%/}"
PAGE_SIZE="${DEBRIS_PAGE_SIZE:-200}"
MAX_PAGES="${DEBRIS_MAX_PAGES:-50}"
SUMMARY_ROW_CAP="${DEBRIS_SUMMARY_ROWS:-300}"

# The identifier prefixes. The first five are what the tests and examples put
# in ids and names: tg.Name() bases (tfprovider-, tf-acc-, rt-source, RT_) and
# the examples' tf-example- convention. The last two are the dataset fixtures
# EnsureDatasetEntity provisions (tf_contract_test.<name>, tf_assertion_test.),
# which are hard-deleted on cleanup and so only linger after a crash.
PREFIX_RE='tfprovider-|tf-acc-|tf-example-|RT_|rt-source|tf_contract_test[.]|tf_assertion_test[.]'
ID_RE="(^|[\":(,/]|%3A)(${PREFIX_RE})"

# Entity types to scan, as "<openapi path>[:<aspects to fetch>]". An empty
# aspect list fetches every aspect, which is what lets a name-only match be
# found on a UUID-keyed entity (assertions, monitors, policies). Datasets fetch
# only `status`: the instance holds many of them, and the fixture names are in
# the URN anyway.
#
# Deliberately not scanned: globalsettings and datahubremoteexecutorglobalconfig
# (singletons the tests modify in place rather than create) and datahubrole
# (fixed system roles).
TYPES=(
  datahubsecret
  datahubingestionsource
  datahubconnection
  datahubremoteexecutorpool
  datahubaction
  datahubpolicy
  datahubpagetemplate
  datahubpagemodule
  oauthauthorizationserver
  structuredproperty
  form
  test
  tag
  domain
  dataproduct
  glossarynode
  glossaryterm
  ownershiptype
  corpgroup
  corpuser
  assertion
  monitor
  assertionassignmentrule
  datacontract
  "dataset:status"
)

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

CANDIDATES="$TMP/candidates.tsv"
ERRORS="$TMP/errors.tsv"
: > "$CANDIDATES"
: > "$ERRORS"
: > "$OUT"

SCANNED_TYPES=0
SCANNED_ENTITIES=0
TRUNCATED=""

# http_get URL [curl args...] -- sets HTTP_CODE, BODY, CURL_EXIT. Never -sf:
# an HTTP error must stay distinguishable from an empty result.
http_get() {
  local url=$1
  shift
  local resp
  resp=$(curl -sS --max-time 60 -w "\n%{http_code}" \
    -H "Authorization: Bearer ${DATAHUB_GMS_TOKEN}" \
    -H "Accept: application/json" \
    "$@" "$url" 2>&1)
  CURL_EXIT=$?
  HTTP_CODE=$(printf '%s' "$resp" | tail -1)
  BODY=$(printf '%s' "$resp" | sed '$d')
}

# One line for an error table: type, HTTP status, the first 200 characters of
# the response with newlines and tabs flattened. Error bodies are server
# messages, not entity content.
record_error() {
  local type=$1
  local snippet
  snippet=$(printf '%s' "$BODY" | tr '\n\t|' '   ' | cut -c1-200)
  printf '%s\t%s\t%s\n' "$type" "${HTTP_CODE:-000} (curl exit ${CURL_EXIT})" "$snippet" >> "$ERRORS"
}

scan_type() {
  local spec=$1
  local type=${spec%%:*}
  local aspects=""
  [ "$type" != "$spec" ] && aspects=${spec#*:}

  local scroll="" page=0 n
  while :; do
    page=$((page + 1))
    local args=(-G
      --data-urlencode "count=${PAGE_SIZE}"
      --data-urlencode "query=*"
      --data-urlencode "includeSoftDelete=true"
      --data-urlencode "skipCache=true")
    [ -n "$aspects" ] && args+=(--data-urlencode "aspects=${aspects}")
    [ -n "$scroll" ] && args+=(--data-urlencode "scrollId=${scroll}")

    http_get "${GMS}/openapi/v3/entity/${type}" "${args[@]}"
    if [ "$CURL_EXIT" -ne 0 ] || [ "${HTTP_CODE:-000}" != "200" ]; then
      record_error "$type"
      return
    fi

    if ! n=$(printf '%s' "$BODY" | jq -r '(.entities // []) | length' 2>/dev/null); then
      HTTP_CODE="200 (unparseable)"
      record_error "$type"
      return
    fi
    SCANNED_ENTITIES=$((SCANNED_ENTITIES + n))

    printf '%s' "$BODY" | jq -r --arg re "$ID_RE" --arg type "$type" '
      (.entities // [])[]
      | (.urn // "") as $u
      | (if ($u | test($re)) then "urn"
         elif (tostring | test($re)) then "content"
         else empty end) as $m
      | [$type, $u, $m] | @tsv' >> "$CANDIDATES"

    scroll=$(printf '%s' "$BODY" | jq -r '.scrollId // empty')
    if [ -z "$scroll" ] || [ "$n" -eq 0 ]; then
      break
    fi
    if [ "$page" -ge "$MAX_PAGES" ]; then
      TRUNCATED="${TRUNCATED}${type} "
      break
    fi
  done
  SCANNED_TYPES=$((SCANNED_TYPES + 1))
}

for spec in "${TYPES[@]}"; do
  scan_type "$spec"
done

# Confirm each candidate against the strongly consistent entity endpoint, and
# take its soft-delete state from there rather than from the index.
sort -u "$CANDIDATES" | while IFS=$'\t' read -r type urn match; do
  [ -n "$urn" ] || continue
  enc=$(jq -rn --arg u "$urn" '$u | @uri')
  http_get "${GMS}/openapi/v3/entity/${type}/${enc}" -G --data-urlencode "aspects=status"
  if [ "$CURL_EXIT" -eq 0 ] && [ "$HTTP_CODE" = "404" ]; then
    echo "dropped (gone, index lag): $urn" >&2
    continue
  fi
  state="unverified"
  if [ "$CURL_EXIT" -eq 0 ] && [ "$HTTP_CODE" = "200" ]; then
    if [ "$(printf '%s' "$BODY" | jq -r '.status.value.removed // false' 2>/dev/null)" = "true" ]; then
      state="soft-deleted"
    else
      state="active"
    fi
  fi
  printf '%s\t%s\t%s\t%s\n' "$type" "$urn" "$match" "$state" >> "$OUT"
done

HITS=$(wc -l < "$OUT" | tr -d ' ')
URN_HITS=$(awk -F'\t' '$3 == "urn"' "$OUT" | wc -l | tr -d ' ')
echo "Scanned ${SCANNED_TYPES}/${#TYPES[@]} types, ${SCANNED_ENTITIES} entities; ${HITS} hits (${URN_HITS} by URN)." >&2

[ "$SUMMARY" -eq 1 ] || exit 0

# ---- Markdown summary --------------------------------------------------------

HAVE_BASELINE=0
if [ -n "$BASELINE" ] && [ -r "$BASELINE" ]; then
  HAVE_BASELINE=1
fi

is_new() {
  [ "$HAVE_BASELINE" -eq 1 ] || { echo "?"; return; }
  if awk -F'\t' -v u="$1" '$2 == u { found = 1 } END { exit !found }' "$BASELINE"; then
    echo "no"
  else
    echo "**yes**"
  fi
}

NEW_URN_HITS=0
if [ "$HAVE_BASELINE" -eq 1 ]; then
  NEW_URN_HITS=$(awk -F'\t' 'NR == FNR { seen[$2] = 1; next } $3 == "urn" && !($2 in seen)' "$BASELINE" "$OUT" | wc -l | tr -d ' ')
fi

{
  echo "## Debris report"
  echo
  echo "Read-only scan of \`${GMS}\` for entities carrying a test identifier prefix. Nothing was deleted."
  echo
  echo "- Scanned ${SCANNED_TYPES} of ${#TYPES[@]} entity types, ${SCANNED_ENTITIES} entities."
  echo "- **${URN_HITS}** entities whose URN carries a test prefix (sweepable debris), plus $((HITS - URN_HITS)) that only reference one."
  if [ "$HAVE_BASELINE" -eq 1 ]; then
    echo "- **${NEW_URN_HITS}** of the URN matches were not present before this run's tests started."
  else
    echo "- No pre-run baseline was available, so new and pre-existing debris cannot be told apart."
  fi
  if [ -n "$TRUNCATED" ]; then
    echo "- Stopped after ${MAX_PAGES} pages of ${PAGE_SIZE} for: \`${TRUNCATED% }\`; debris beyond that is not listed."
  fi
  echo
  if [ "$HITS" -gt 0 ]; then
    echo "| Type | URN | Match | State | New this run |"
    echo "|---|---|---|---|---|"
    # URN matches first, then content-only references.
    sort -t$'\t' -k3,3r -k1,1 -k2,2 "$OUT" | head -n "$SUMMARY_ROW_CAP" |
      while IFS=$'\t' read -r type urn match state; do
        echo "| ${type} | \`${urn}\` | ${match} | ${state} | $(is_new "$urn") |"
      done
    if [ "$HITS" -gt "$SUMMARY_ROW_CAP" ]; then
      echo
      echo "Only the first ${SUMMARY_ROW_CAP} of ${HITS} rows are shown."
    fi
    echo
    echo "*Match*: \`urn\` means the entity's own id carries the prefix; \`content\` means only an aspect references such an id (often a real entity pointing at a deleted test entity). *State* comes from the strongly consistent entity endpoint."
  elif [ "$SCANNED_TYPES" -eq 0 ]; then
    echo "**The scan failed: no entity type could be read**, so this says nothing about debris. See the errors below."
  elif [ -s "$ERRORS" ]; then
    echo "No debris found in the types that could be scanned."
  else
    echo "No debris found."
  fi
  if [ -s "$ERRORS" ]; then
    echo
    echo "### Types that could not be scanned"
    echo
    echo "| Type | HTTP | Response (truncated) |"
    echo "|---|---|---|"
    while IFS=$'\t' read -r type code snippet; do
      echo "| ${type} | ${code} | ${snippet} |"
    done < "$ERRORS"
  fi
} >> "${GITHUB_STEP_SUMMARY:-/dev/stdout}"

if [ "$URN_HITS" -gt 0 ] && [ -n "${GITHUB_ACTIONS:-}" ]; then
  if [ "$HAVE_BASELINE" -eq 1 ]; then
    echo "::warning title=Debris on the shared instance::${URN_HITS} test-prefixed entities on ${GMS} (${NEW_URN_HITS} new this run). See the job summary; sweep by hand."
  else
    echo "::warning title=Debris on the shared instance::${URN_HITS} test-prefixed entities on ${GMS}. See the job summary; sweep by hand."
  fi
fi
if [ -s "$ERRORS" ] && [ -n "${GITHUB_ACTIONS:-}" ]; then
  echo "::warning title=Debris report incomplete::$(wc -l < "$ERRORS" | tr -d ' ') entity types could not be scanned. See the job summary."
fi

exit 0
