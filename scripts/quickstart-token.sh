#!/usr/bin/env bash
# Mints a DataHub access token against a local Quickstart via the GMS GraphQL
# endpoint. Works because Quickstart runs with METADATA_SERVICE_AUTH_ENABLED=false
# by default, so the X-DataHub-Actor header is honoured without a prior login.
#
# Prints the token to stdout. Exits non-zero on any failure.

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
if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
  printf 'Token mint succeeded HTTP-wise but response had no accessToken: %s\n' "$BODY" >&2
  exit 1
fi
printf '%s\n' "$TOKEN"
