#!/usr/bin/env bash
# Copyright 2026 The DataHub Project Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

BASEDIR=$(dirname "$0")
TOKEN=$(yq -r '.gms.token' ~/.datahubenv)

if [[ -z "${OPENAPI_URL:-}" ]]; then
	echo "ERROR: OPENAPI_URL is not set." >&2
	echo "Set it via environment variable, e.g.:" >&2
	echo "OPENAPI_URL=\"https://<your-datahub-url>/openapi/v3/api-docs/openapi-v3\" \"$0\"" >&2
	exit 1
fi

# # Download openapi definition
mkdir -p "${BASEDIR}/def"
curl -H "Authorization: Bearer $TOKEN" -o "${BASEDIR}/def/api-docs.json" "$OPENAPI_URL"

# # Generate Go client
docker run --network host --user $(id -u):$(id -g) --rm \
	-v "${PWD}:/local" \
	-v "${PWD}/def/api-docs.json:/tmp/api-docs.json" \
	openapitools/openapi-generator-cli generate \
	-i /tmp/api-docs.json \
	-g go \
	-o /local/out/go
