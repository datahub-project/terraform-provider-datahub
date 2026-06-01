# datahub_local_user_login: OSS vs Cloud Differences

This document captures the empirically verified behavioral differences between OSS DataHub and DataHub Cloud for the `datahub_local_user_login` resource. All findings were discovered through live testing against OSS Quickstart and the `demo.gcp.acryl.io` Cloud instance in June 2026.

Read this before modifying `local_user_login_resource.go`, `native_users.go`, or any test scenario that exercises the signUp flow.

## Overview

The `datahub_local_user_login` resource provisions a native-auth login for a DataHub user by orchestrating three distinct API calls: `getInviteToken` (GraphQL), `POST /signUp` (REST), and `createNativeUserResetToken` (GraphQL). The first and third calls are identical between OSS and Cloud. The second diverges substantially.

## signUp endpoint differences

| Property | OSS | Cloud |
|---|---|---|
| Endpoint path | `POST <gms_url>/auth/signUp` | `POST <base_url>/signUp` |
| Framework | Spring MVC (`@RequestMapping("/auth")` on metadata-service) | Play Framework (frontend proxy) |
| `/auth/signUp` on Cloud | - | Returns 404 |
| `/signUp` on OSS Quickstart | Returns 200 with empty body (React SPA catch-all) — silently does nothing | Correct endpoint |

The provider tries `/auth/signUp` first; on 404 it falls back to `/signUp`.

## Authorization header

Both OSS and Cloud accept a Bearer token on the `/signUp` request. Earlier investigation suggested the auth header caused 500 errors on Cloud — this was wrong. The Python SDK (`DataHubGraph.create_native_user`) sends a Bearer token to Cloud's `/signUp` and succeeds. The 500 errors we saw were caused by other issues (wrong payload shape, stale invite tokens) not the auth header.

## Invite token

Both `/api/graphql` and `/api/v2/graphql` with Bearer token auth return the same invite token on both platforms. The browser uses session-cookie auth on `/api/v2/graphql` to get a session-scoped token, but PAT-based `getInviteToken` returns a token that is equally valid for Cloud's `/signUp`. No special token handling is needed.

## Request payload

The same payload works on both platforms:

```json
{
  "userUrn":     "urn:li:corpuser:<username>",
  "fullName":    "Alice Smith",
  "email":       "alice@example.com",
  "password":    "<password>",
  "title":       "Other",
  "inviteToken": "<token>"
}
```

An earlier hypothesis that Cloud required `getDataHubUpdates: false` and rejected `userUrn` and `title` was incorrect. The Python SDK sends this exact shape and succeeds on Cloud.

## URN derivation — the most important difference

| Platform | URN of created user |
|---|---|
| OSS | `urn:li:corpuser:<username>` (uses `userUrn` field from payload) |
| Cloud | `urn:li:corpuser:<email>` (ignores `userUrn`, derives from `email` field) |

**Consequence for operators:** On Cloud, non-SSO local users always have their email address as their DataHub username. Set `username` equal to the email address in your HCL config:

```hcl
resource "datahub_local_user_login" "alice" {
  username  = "alice@example.com"
  full_name = "Alice Smith"
  email     = "alice@example.com"
}
```

**Consequence for the provider:** After signUp, `GetUserByURN` is called to confirm entity creation. On Cloud, the entity lives at `urn:li:corpuser:<email>`, not `urn:li:corpuser:<username>`. The provider polls both URNs and stores whichever one the server actually used as `user_urn` in state.

## signUp response body

Both platforms return HTTP 200 on success. OSS returns a JSON body; Cloud returns an **empty body**. The provider must not treat an empty 200 as a failure.

## After-signUp propagation delay

On both platforms, `POST /signUp` internally calls `ingestProposal` to write the user aspects (`corpUserInfo`, `corpUserStatus`, `corpUserCredentials`). This is asynchronous relative to the HTTP response. The entity may not be immediately readable via the OpenAPI v3 endpoint. The provider polls with linear back-off (up to 10 attempts, 0.5–5s spacing).

`createNativeUserResetToken` also requires the credentials aspect to be committed before it succeeds, and is subject to the same lag.

## NativeUserService signUp guard

The guard that determines whether a user entity blocks signUp differs:

| Platform | Condition for rejection |
|---|---|
| OSS | Entity exists at all (regardless of credentials) |
| Cloud | Entity exists AND already has credentials |

Cloud's guard allows adding native credentials to an existing catalog-record user (e.g. one provisioned by metadata ingestion or SSO JIT). OSS rejects this. The provider surfaces a clear diagnostic when the OSS guard fires.

## Import behavior

| Operation | OSS | Cloud |
|---|---|---|
| Import by bare username | Works: resolves to `urn:li:corpuser:<username>` | Fails: user lives at `urn:li:corpuser:<email>` |
| Import by full URN | Works | Works |
| `username` attribute after import | Matches config | Returns the email (e.g. `alice@example.com`), not the config username |

For this reason, import test scenarios use `ImportStateIdFunc` (from state's `user_urn`) rather than a bare username, and `username` is in `ImportStateVerifyIgnore`.

## Test scenario requirements

Because Cloud derives the URN from the email field:

1. **Emails must be unique per test run.** Hardcoded emails (e.g. `reset@example.com`) cause repeated runs to collide: the same email produces the same URN, and if a previous test left a user with credentials, the next signUp is rejected. Use `username + "@example.com"` as the email pattern.

2. **`user_urn` state checks must use `NotNull()`**, not `StringExact("urn:li:corpuser:<username>")`, because Cloud will produce a different URN.

## Reference

- Client implementation: `internal/provider/pkg/datahub/native_users.go` — see the `SignUp` method godoc for the authoritative summary.
- Resource: `internal/provider/local_user_login_resource.go`
- Test scenarios: `internal/provider/datahubtesting/scenarios.go` — `LocalUserLogin*` functions
- Memory: `~/.claude/projects/-Users-brettrandall-src-terraform-provider-datahub/memory/feedback_signup_endpoint_routing.md`
