#!/usr/bin/env bash
# Mints a DataHub access token against a local Quickstart via the GMS GraphQL
# endpoint. Works because Quickstart runs with METADATA_SERVICE_AUTH_ENABLED=false
# by default, so the X-DataHub-Actor header is honoured without a prior login.
#
# If createAccessToken returns 403 (the resolver enforces its own privilege
# check even when the auth middleware is disabled), we fall back to a
# placeholder token. With METADATA_SERVICE_AUTH_ENABLED=false the GMS accepts
# any non-empty Bearer token for regular API operations, so the acceptance
# tests run correctly against the placeholder.
#
# Prints the token to stdout. Exits non-zero on unexpected failures.

set -euo pipefail

GMS_URL="${DATAHUB_GMS_URL:-http://localhost:8080}"
ACTOR="${TOKEN_ACTOR:-urn:li:corpuser:datahub}"
TOKEN_NAME="${TOKEN_NAME:-terraform-provider-datahub-acc}"
TOKEN_DURATION="${TOKEN_DURATION:-ONE_HOUR}"

PAYLOAD=$(printf '{"query":"mutation createAccessToken($input: CreateAccessTokenInput!) { createAccessToken(input: $input) { accessToken metadata { id } } }","variables":{"input":{"type":"PERSONAL","actorUrn":"%s","duration":"%s","name":"%s"}}}' \
  "$ACTOR" "$TOKEN_DURATION" "$TOKEN_NAME")

HTTP_RESPONSE=$(curl -sS -w "\n%{http_code}" \
  -H "Content-Type: application/json" \
  -H "X-DataHub-Actor: ${ACTOR}" \
  -X POST "${GMS_URL}/api/graphql" \
  --data "${PAYLOAD}" 2>&1)
CURL_EXIT=$?
HTTP_CODE=$(printf '%s' "$HTTP_RESPONSE" | tail -1)
BODY=$(printf '%s' "$HTTP_RESPONSE" | sed '$d')

if [ "$CURL_EXIT" -ne 0 ] || [ "${HTTP_CODE:-000}" -ge 400 ]; then
  printf 'Token mint failed (exit=%s, HTTP=%s): %s\n' "$CURL_EXIT" "$HTTP_CODE" "$BODY" >&2
  exit 1
fi

TOKEN=$(printf '%s' "$BODY" | jq -r '.data.createAccessToken.accessToken // empty')
if [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; then
  printf '%s\n' "$TOKEN"
  exit 0
fi

# createAccessToken returned HTTP 200 but no token -- usually a GraphQL-level
# 403. This happens when METADATA_SERVICE_AUTH_ENABLED=false: the auth
# middleware is bypassed for regular API calls, but the createAccessToken
# resolver still enforces its own privilege check. Fall back to a placeholder;
# the GMS will accept it for all acceptance test operations.
ERROR_CODE=$(printf '%s' "$BODY" | jq -r '.errors[0].extensions.code // empty' 2>/dev/null || true)
if [ "$ERROR_CODE" = "403" ]; then
  printf 'createAccessToken returned 403 (auth middleware disabled but resolver gated); using placeholder token\n' >&2
  printf 'quickstart-no-auth\n'
  exit 0
fi

printf 'Token mint succeeded HTTP-wise but response had no accessToken: %s\n' "$BODY" >&2
exit 1
