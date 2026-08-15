# AI plugins and outbound OAuth: `datahub_ai_plugin` + `datahub_oauth_authorization_server`

Maintainer-facing design notes for the two coupled Cloud-only resources that let Terraform register an Ask DataHub AI plugin (today: an MCP server) and the outbound OAuth authorization server it authenticates through.

Status: PR 1 (`datahub_oauth_authorization_server`) implemented; PR 2 (`datahub_ai_plugin`) not started. Prerequisite reading: [datahub-model-and-resource-design.md](datahub-model-and-resource-design.md) (read in full), [provider-org-settings.md](provider-org-settings.md) (granularity rule), `CLAUDE.md` (new-resource checklist, Cloud-only rules, provider-defaults checklist item).

**Nothing in this document was verified against a live DataHub instance.** Every claim about server behaviour is read from the OSS repo and the closed Cloud fork at the revisions noted in "Sources". The "Open questions" section lists what must be probed before implementation, and says what a different answer would change.

## What prompted it

The Woodside demo (`~/src/datahub-demos/woodside`) hand-scripts exactly this. `scripts/setup_ai_plugin.py` calls `upsertOAuthAuthorizationServer` then `upsertAiPlugin` against fixed ids (`wds-snowflake-oauth`, `wds-snowflake-mcp`) for idempotency, registering an MCP-server plugin pointed at a Snowflake-managed MCP server. `snowflake_mcp.tf` manages the Snowflake half in Terraform and its header comment records the constraint that shaped this design:

> The OAuth client id/secret are deliberately NOT read into Terraform state: `scripts/setup_ai_plugin.py` fetches them live (`SYSTEM$SHOW_OAUTH_CLIENT_SECRETS`) and registers the plugin via `upsertAiPlugin`, where the secret is encrypted server-side.

`DEMO-AI.md` section 3b states the gap plainly ("Terraform status: not supported yet - no ai-plugin resource in the provider") and nominates the WriteOnly treatment from `datahub_connection`.

The demo is in-flight. It is evidence of intent, not a specification, and it contains at least one thing this design contradicts - see "What the demo left ambiguous".

The roadmap ranked these inconsistently: `upsertOAuthAuthorizationServer` is **HIGH** ("cleanest config object in the access space", Category 4 and Tier 3 item 15), while `upsertAiPlugin`/`deleteAiPlugin` sat in the Category 11 IRRELEVANT bucket as "borderline; revisit if customers ask". The ask arrived, and it makes the HIGH item a hard prerequisite of the borderline one. Roadmap edits are listed at the end.

## Sources

| Repo | Revision | What was read |
|---|---|---|
| `terraform-provider-datahub` (this worktree) | `feat/ai-plugin` | `secret_resource.go`, `connection_resource.go`, `organization_display_preferences_resource.go`, `defaults.go`, `importtarget/`, `pkg/datahub/keyedmutex.go`, `pkg/datahub/organization_display_preferences.go` |
| `datahub` (OSS) | `v1.6.0rc1-811-gf4bc6383ff` | `entity-registry.yml`, `datahub-graphql-core/src/main/resources/`, `metadata-ingestion/.../urn_defs.py`, `utilities/urn_encoder.py` |
| `datahub-fork` (closed Cloud fork - findings paraphrased, never quoted) | `fdb870497a9` | AI-plugin and OAuth GraphQL schema, the four upsert/delete resolvers, the cascade helper, the AI-plugin PDL, `entity-registry.yml`, the OpenAPI v3 generic entity controller, `ConfigEntityRegistry` |

## Scope

Two resources:

1. **`datahub_oauth_authorization_server`** - an outbound OAuth client configuration (DataHub as the OAuth *client*, calling an external API). Backed by `upsertOAuthAuthorizationServer` / `deleteOAuthAuthorizationServer` on the `oauthAuthorizationServer` entity type.
2. **`datahub_ai_plugin`** - a tenant-level Ask DataHub plugin. Backed by `upsertAiPlugin` / `deleteAiPlugin`. One plugin is a `service` entity *plus* one entry in the `globalSettingsInfo.aiPluginSettings.plugins` array on the `globalSettings` singleton.

Explicitly out of scope, and why:

- **`datahub_ai_settings`** (the `aiAssistant` / `documentationAi` / `aiContext` / `mcpSettings` singleton) - separate roadmap item; this design only constrains it (see "Cross-resource coupling").
- **Per-user AI state** - `updateUserAiPluginSettings`, the per-user "Connect" OAuth flow and the per-user `dataHubConnection` it mints, `UserAiPluginConfig` on `corpUserSettings`. This is the org-versus-user trap `provider-org-settings.md` warns about, and it recurs here exactly as predicted. The plugin resource *creates the conditions for* per-user state and its destroy *deletes* that state server-side, but it never manages it.
- **`mcpServers`** (the inbound side) - a sibling list on the same singleton, same child-resource shape, not this change.
- **Owners on either entity** - both register `ownership`, and setting ownership on a config entity the provider manages is legitimate per the model doc, but no sibling resource exposes a first-class `owners` attribute today. Deliberate non-goal; note it so it is not a silent omission.
- **`SHARED_API_KEY` auth** - see "Auth types" below. Deferred, with the schema shaped so adding it is additive.

### Sequencing decision

**Build in sequence - OAuth server first - but release both in the same minor version. Two PRs, one CHANGELOG entry, one runnable example.**

Build the OAuth server first because:

1. **It is a hard prerequisite, not a nice-to-have.** The plugin's `USER_OAUTH` mode references the server by `oauthServerUrn`. The only alternative the API offers is `newOAuthServer`, an inline-create input that *refuses* a caller-supplied `id` and always mints a random UUID - which the provider's URN-determinism rule forbids outright. So `newOAuthServer` is unusable and the standalone resource is the only path.
2. **It is the smaller unit carrying the riskier new mechanic.** One entity, one aspect, one mutation, one read. But it owns the entire WriteOnly-secret problem - in-place rotation, presence-only drift, the orphaned-secret trail. Proving that on a resource with no other moving parts keeps the plugin PR about the plugin.
3. **The plugin PR is genuinely larger:** a two-source Read (entity + singleton), read-modify-write serialisation on a shared singleton, a two-part "is this actually a plugin" guard on a shared entity type, and bidirectional cascade-delete semantics.

Release them together because:

4. **The OAuth server alone is inert.** The only consumer of an `oauthAuthorizationServer` anywhere in DataHub today is `AiPluginConfig.oauthConfig.serverUrn`. Shipping it alone gives users a Cloud-only resource that changes nothing observable - and this provider's convention (`docs/guides/` per feature, outputs that let a user verify an apply) does not survive a resource with nothing to verify.
5. **The bugs live in the interaction.** `deleteAiPlugin` can delete the OAuth server; `deleteOAuthAuthorizationServer` rewrites referencing plugins. Neither is testable in isolation, and neither can be documented honestly without the other.

This does not contradict the roadmap's independently-HIGH rating for item 15. It refines it to: high priority to *build*, not shippable *alone*.

## The two entities

### Shapes

| | `oauthAuthorizationServer` | `service` |
|---|---|---|
| URN | `urn:li:oauthAuthorizationServer:<id>` | `urn:li:service:<id>` |
| Key aspect | `oauthAuthorizationServerKey` (single `id` field) | `serviceKey` (single `id` field) |
| Registry category | `core` | `core` |
| In OSS entity registry? | **No** - fork only | **Yes** - `category: core`, as of v1.6.0rc1 |
| OSS Python SDK URN class? | No | **Yes** - `ServiceUrn(id)`, single-part |
| Aspects the provider writes | `oauthAuthorizationServerProperties` | `serviceProperties`, `subTypes`, `mcpServerProperties` |
| Other registered aspects | `ownership`, `status`, `semanticContent` | `serviceDefinition`, `incidentsSummary`, `ownership`, `status`, **`globalTags`**, `dataPlatformInstance`, `semanticContent` |
| GraphQL mutations | fork only | fork only |

Note the split stability posture: both entity types are `category: core` (not `internal`), but the plugin's *configuration half* lives on the `globalSettings` singleton, which is `category: internal`. The weakest link governs. See "OSS vs Cloud availability".

### The plugin is not one thing

`upsertAiPlugin` is a workflow mutation, not an entity upsert - the fork's own schema comment says so, and says the names `upsertService`/`deleteService` are deliberately reserved for a future pure-entity API. One call writes:

- `serviceProperties` (displayName, description) on `urn:li:service:<id>`
- `subTypes` on the same URN
- `mcpServerProperties` (url, transport, timeout, customHeaders) on the same URN
- one entry in `globalSettingsInfo.aiPluginSettings.plugins[]` on `urn:li:globalSettings:0`, keyed by `serviceUrn`, carrying `enabled`, `instructions`, `authType`, and the auth sub-config (`oauthConfig.serverUrn` + `requiredScopes`, or the API-key variants)
- optionally an `oauthAuthorizationServer` (the `newOAuthServer` path - the provider never uses it)
- optionally a `dataHubConnection` holding a shared API key (the `SHARED_API_KEY` path - deferred)

The array-entry `id` is set to the service URN string, not to the user's `id`. So the identity of a plugin, for every read and cascade lookup, is the **service URN**.

This is a shape the provider has not modelled before: **a member of a list inside a singleton aspect.** It is not a singleton resource (there are many) and not a plain entity resource (half its state is elsewhere). Name it as a pattern, because `mcpServers` will need it too.

## Design checklist

The checklist from `CLAUDE.md` / `datahub-model-and-resource-design.md`, answered explicitly.

### URN strategy

**Format and key source.** Both take a user-supplied `id`: `urn:li:oauthAuthorizationServer:<id>` and `urn:li:service:<id>`. Both are Required and force replacement on change.

**SDK convention.** `service` is confirmed: the OSS Python SDK's `ServiceUrn(id)` is a single-part URN over the same `serviceKey.id`, with the same id semantics - so a Terraform-managed plugin and an SDK-created service with the same id converge on one entity rather than duplicating. For `oauthAuthorizationServer` there is **no SDK convention to match** - the entity is absent from the OSS registry and no generated Python URN class for it was found in either repo. The provider defines the convention, and the format it must use is fixed anyway, because it is exactly what the resolver constructs. State this as "no conflicting convention exists", not as "verified match".

**Non-deterministic paths that exist and must be avoided.** Three, all of them real:

1. Omitting `id` on `upsertAiPlugin` -> random UUID service id.
2. Omitting `id` on `upsertOAuthAuthorizationServer` -> random UUID server id.
3. `newOAuthServer` inline creation -> **always** a random UUID, and it raises an error if you supply an id at all.

Consequence: the provider always sends an explicit `id` on both mutations, and **never uses `newOAuthServer`**. That is a hard rule, not a preference. Worth a unit test asserting the marshalled input never contains the field.

**Id validation (plan-time).** Match the SDK's `UrnEncoder.contains_reserved_char`: reject the URN-reserved characters `,`, `(`, `)`, and the unit-separator U+241F. Also reject empty, leading/trailing whitespace, and a value already starting `urn:li:` (the SDK coerces such input; the provider should reject it rather than double-prefix). New hand-rolled validator in the `enumvalidators.go` / `mapvalidators.go` style - the project convention is not to take the framework-validators dependency.

**No container URNs involved.** Neither entity references a container. Not applicable.

**Coexistence.** UI-created plugins and OAuth servers carry UUID ids. They coexist fine - Terraform manages only its own deterministic ids - and can be imported, leaving the UUID as the id (adopt-computed), the same posture recorded for `datahub_service_account`.

### Reference and dependency modelling

**`datahub_ai_plugin` -> `datahub_oauth_authorization_server`** is a real reference, expressed as `oauth = { server_urn = datahub_oauth_authorization_server.x.urn }`. A raw URN string is the documented escape hatch.

The usual caveat ("raw strings bypass validation") understates the risk here. Because both delete paths cascade into the *other* resource, the dependency edge is what keeps `terraform destroy` from rewriting the other resource's server-side state behind Terraform's back. Document raw strings as forfeiting ordering, not merely validation. Loose coupling still applies for creation ordering - DataHub accepts a reference to a nonexistent server - so the edge buys correctness on *destroy*, which is the reverse of the usual argument.

**`datahub_ai_plugin` -> `globalSettings` singleton (read coupling).** The plugin's Read must read the singleton. So does `datahub_organization_display_preferences` (a different section) and so will `datahub_ai_settings`. There is no *write* conflict, because all of these go through mutations that read-modify-write the aspect server-side rather than replacing it wholesale - the same finding that made `datahub_organization_display_preferences` viable. See "Cross-resource coupling" for the constraint this places on `datahub_ai_settings`.

### Upsert and list semantics

Both resources own the complete value of every list and map they expose, and POST it in full on every write. No PATCH, no append.

But the per-field null semantics of `upsertOAuthAuthorizationServer` are **not uniform**, and getting this wrong is silent. Read from the resolver, the properties aspect is rebuilt from scratch on every call, with a per-field fallback:

| Field | `null` in input means |
|---|---|
| `clientId`, `authorizationUrl`, `tokenUrl` | **preserve** the stored value |
| `clientSecret` | **preserve** the stored secret (this one is the feature) |
| `scopes` | preserve; but an **empty array clears** |
| `description`, `additionalTokenParams`, `additionalAuthParams`, `authScheme`, `authQueryParam` | **clear** (no fallback branch) |
| `displayName` | required, cannot be null |
| `tokenAuthMethod`, `authLocation`, `authHeaderName` | fall back to a hard-coded server default (`POST_BODY`, `HEADER`, `Authorization`) |

One correction found at implementation time (PR 1): the table above describes the *resolver's* null handling, but four input fields also carry **SDL literal defaults** (`tokenAuthMethod = POST_BODY`, `authLocation = HEADER`, `authHeaderName = "Authorization"`, `authScheme = "Bearer"`) that graphql-java injects when the field is *omitted from the variables* - an explicit null suppresses them. So for `authScheme`, omission writes `"Bearer"` while explicit null clears; "clear on null" was right, "clear when not sent" would have been wrong.

Consequence: the provider must send **every** field it exposes explicitly on every write. Omitting a preserve-on-null field silently keeps a stale value (looks like the provider lost the write); omitting a clear-on-null field silently wipes it; omitting an SDL-defaulted field silently writes the default. Sending everything makes all three behaviours correct and matches the provider's full-ownership convention. Worth an explicit unit test per field (implemented: `TestUpsertOAuthAuthorizationServerVariables_*` in `oauth_authorization_servers_test.go`).

For `upsertAiPlugin`, the per-field semantics are similar, with one field that **cannot be cleared**: `requiredScopes` is only overwritten when the input list is non-null *and* non-empty; otherwise the existing value is preserved. So `required_scopes = []` is likely a no-op, not a clear. This is the one place full-list ownership is not achievable. See Open question 3 - and note the asymmetry with the OAuth server's own `scopes`, which *is* clearable.

### Delete behaviour

**Both deletes are hard deletes.** Both resolvers call `entityClient.deleteEntity`, which resolves to `entityService.deleteUrn` - removal of the URN's aspects from the primary store, not a `status.removed` soft delete. Both entity types *register* `status`, which makes the assumption easy to get backwards.

Consequences: no soft-delete resurrection risk, no stale-aspect leakage on recreate, no need for a `force_hard_delete` escape hatch. This is the opposite of the model doc's default assumption and should be stated in both resources' docs.

**`deleteAiPlugin` cascades, in this order:** removes the entry from `globalSettings`; deletes the shared-API-key `dataHubConnection` if the auth type used one; **deletes the referenced OAuth authorization server if no other plugin references it** (including its client-secret `dataHubSecret`); deletes per-user OAuth connections and per-user plugin settings for every user who had connected; hard-deletes the `service` entity.

**`deleteOAuthAuthorizationServer` cascades:** finds every plugin referencing it, **sets each one's `authType` to `NONE` and drops its `oauthConfig`**, writes the singleton back; deletes the current client-secret `dataHubSecret`; cleans per-user OAuth connections for the affected plugins; hard-deletes the server.

Cascade steps other than the singleton update are best-effort - the fork logs failures and continues rather than propagating, deliberately, because the cascade is not transactional. So a delete that reports success can leave orphans.

Four Terraform-visible consequences:

1. **Destroying the plugin can delete a resource Terraform still has in state.** The OAuth server's `Read` must treat 404 as "gone" and `RemoveResource`; its `Delete` must treat not-found as success. Non-negotiable, and it is the reason `terraform destroy` of the whole config works cleanly (plugin first, then the server delete is a no-op).
2. **Removing only the plugin from config strands the server resource.** The plugin destroy deletes the entity; the next plan sees the server missing and wants to recreate it. Documented, not preventable.
3. **`-target`ed or out-of-order destroys rewrite the other resource's state.** Destroying only the OAuth server flips the plugin's `authType` to `NONE` server-side; the next plan reports that as drift on the plugin and re-applies. Converges, but the intermediate state is a live plugin with no auth.
4. **Renaming an `id` is a destroy-and-create with a broken window.** Replacing the OAuth server destroys it (flipping the plugin to `NONE`), creates the new URN, then the plugin's in-place update restores the reference. It converges because the dependency edge orders it, but there is a window where the plugin is misconfigured. Document; do not try to engineer around it.

### Provider scope

Both are platform-level configuration: tenant-wide, admin-owned, gated by a single privilege (`MANAGE_CONNECTIONS` - reused for both mutations, since the fork treats these as external connections), slow-moving, not per-asset enrichment, not ingestion output. In scope.

The per-user half is out, per the scope rule. The plugin resource is the org-wide declaration ("this MCP server is available, here is how it authenticates, here are the instructions"); the per-user "Connect" click that mints a user's own OAuth grant is individual state.

### Org-level settings and granularity

Already decided and confirmed by the code: `aiPlugins` is an array of objects with independent lifecycles, so `datahub_ai_plugin` is a child resource, not an attribute on a fat `datahub_ai_settings`. Recorded in `docs/roadmap.md` and `provider-org-settings.md`. Nothing here reopens it. The array is keyed by `serviceUrn`, which gives each member a stable identity - exactly the property that makes a child resource work.

### Provider-level defaults

The checklist asks whether the entity type carries `customProperties`, `globalTags`, or a structured-property aspect. Answering both ways, because omission here is silent:

**`service` -> `datahub_ai_plugin`: `globalTags` YES, `customProperties` NO, structured properties NO.** The registry lists `globalTags` on `service` (in both the fork and OSS). It does *not* list `structuredProperties` - that aspect is enumerated per-entity in the registry, not attached globally, and `service` is not one of the entities that declare it. No `customProperties`-bearing aspect either. (The GraphQL `Service` type declares `structuredProperties` and `institutionalMemory` fields, which is misleading: the registry, not the GraphQL type, decides what a write accepts.)

So `datahub_ai_plugin` is a **`defaults.tags` candidate** and needs the same wiring as the assertion resources - the mechanically identical `{CustomProperties: false, StructuredProperties: false, Tags: true}` row. Concretely:

- new `kindService` in the `entityKind` enum and in `allEntityKinds` (the matrix-completeness test asserts a row per kind)
- a `defaultsSupport` row
- an entry in the kind-to-entity-path map (path `"service"`)
- a `tags_all` attribute via `tagsAllSchema()`
- `ModifyPlan` -> `planTagsAll`
- `Create` -> `resolveAndVerifyTags` **before any write** (the 0.17.0 fix: verifying after the write orphans the entity), then `SetGlobalTags`
- `Read` -> `readTagsAll`
- `Update` -> `resolvePlannedTagsAll` + `ensureTagsExist` + `SetGlobalTags` when `tags_all` differs
- `ImportState` -> `importTagsAll`
- doc/table updates: `defaults.tags` description in `provider.go`, the matrix in `docs/guides/provider-defaults.md`, regenerated `docs/index.md`

The `globalTags` ownership latch means this costs nothing when `defaults.tags` is unset - the provider neither reads nor writes tags until a user opts in.

**`oauthAuthorizationServer` -> `datahub_oauth_authorization_server`: none of the three.** Its only registered aspects are `oauthAuthorizationServerProperties`, `ownership`, `status`, `semanticContent`. It belongs in the "everything else - documented no-op" row of the guide's matrix, alongside `datahub_secret` and `datahub_policy`, with **no** `entityKind` row.

**Caveat:** both answers are read from the fork's registry at `fdb870497a9`. The deployed Cloud build may differ, and a `globalTags` write to an entity type that does not register it is rejected or silently dropped. Probe listed below.

If the tags wiring is deferred to a follow-up PR, that deferral must be recorded in the roadmap and the guide's matrix. Deferring it silently is precisely the failure mode the checklist exists to prevent.

### Import target registry

Both resources need entries in `internal/provider/importtarget/targets/targets.go` or the coverage test fails CI.

**`datahub_ai_plugin`** - enumerate from `globalSettingsInfo.aiPluginSettings.plugins[].serviceUrn`, read via the OpenAPI v3 `globalsettings` endpoint. This is better than the obvious choice of `listServices`, for two reasons: it is strongly consistent (no OpenSearch lag), and it returns exactly the managed set. `listServices` would surface `service` entities that are *not* AI plugins - the fork's Agent Registry work added LangChain/ADK auto-registration of services, so a tenant can hold catalogued services that this resource cannot manage. Generating import blocks for those would fail the whole `generate-config-out` pass. `IDFromURN` strips `urn:li:service:`. `OSSCompatible: false`. Companion data source `datahub_ai_plugins`.

**`datahub_oauth_authorization_server`** - `listOAuthAuthorizationServers` exists and is on the roadmap's confirmed-live-on-Cloud list, so enumeration is available; document the eventual-consistency lag as with every other `list*`-backed enumerator. `IDFromURN` strips `urn:li:oauthAuthorizationServer:`. `OSSCompatible: false`. Companion data source `datahub_oauth_authorization_servers`. (The conservative alternative, `Enumerate: nil` like `datahub_remote_executor_pool`, is defensible if the live probe shows the list query is unreliable - but enumeration here is *not* the `datahub_organization_display_preferences` case, where it was withheld on ownership grounds. There is no full-reset hazard in generating an import for a named OAuth server.)

**Ordering matters.** The file's init() carries a load-bearing comment: `datahub_secret` is registered last because its `value` is Required+WriteOnly, which makes `terraform plan -generate-config-out` exit non-zero and stop generating for every type after it. This design keeps `client_secret_wo` **Optional**, not Required - which is correct on its own terms (`tokenAuthMethod = NONE` is a legitimate public-client configuration with no secret) and avoids adding a second landmine to that ordering constraint. `datahub_ai_plugin` has no Required+WriteOnly attribute either.

## The WriteOnly secret, and what it costs

This is the constraint that shaped the design. The demo keeps `clientId`/`clientSecret` out of Terraform state entirely; a naive resource would persist `clientSecret` and regress that.

### What the server does with the secret

`upsertOAuthAuthorizationServer` does not store the secret on the entity. It encrypts the value via the same `SecretService` that backs `datahub_secret`, writes it as a **new `dataHubSecret` entity** whose id is `<serverId>_clientSecret_<random-uuid>`, and stores only that secret's URN on the properties aspect. The aspect exposes `hasClientSecret` (boolean) and `clientSecretUrn`; the plaintext is never returned by any read.

Three behaviours follow, and only the first is obvious:

1. **`null` preserves, empty string clears, non-empty writes new.** Explicitly documented in the fork's resolver and directly usable by the provider.
2. **Every non-empty write creates a *new* secret entity and orphans the old one.** Nothing deletes the previous secret. The only cleanup path is `deleteOAuthAuthorizationServer`, which deletes the *current* one. So a rotation trail of orphaned encrypted secrets accumulates in the tenant permanently, and the provider cannot clean it up.
3. **Presence is readable even though the value is not.** `hasClientSecret` / `clientSecretUrn` give coarse but genuine drift detection - something neither `datahub_secret` nor `datahub_connection` has.

### Schema

| Attribute | Kind | Notes |
|---|---|---|
| `client_secret_wo` | Optional, **WriteOnly**, Sensitive | Never in state. Framework nullifies it automatically; read from `req.Config`, not `req.Plan`. |
| `client_secret_wo_version` | Optional, Int64 | Rotation trigger. **Not** `RequiresReplace` - see below. |
| `has_client_secret` | Computed, Bool | From the server. The only value-adjacent drift signal. |
| `client_secret_urn` | Computed, String | Makes the rotation trail visible; useful when triaging. |
| `client_id` | Optional, Sensitive, **not** WriteOnly | See "The client_id decision". |

### Rotation is in-place, not replacement

`datahub_secret` and `datahub_connection` both make their `*_wo_version` bump force a **replacement** (`RequiresReplace` / `RequiresReplaceIfConfigured`), because their underlying APIs have no clean in-place secret update. This resource must **not** copy that.

Two reasons. First, the upsert genuinely supports in-place secret replacement - replacement would be gratuitous. Second, and decisively, replacement here means *destroying the OAuth server*, which cascades: every referencing plugin has its `authType` flipped to `NONE` and its `oauthConfig` dropped. Rotating a client secret would silently break every plugin using it, then repair it on the plugin's next update. That is unacceptable for a routine rotation.

So: `client_secret_wo_version` triggers an ordinary in-place `Update`. This is a deliberate divergence from the two existing WriteOnly resources and must be called out in the resource docs, because a user who knows `datahub_secret` will expect a replacement plan.

### The Update rule that is easy to get wrong

Because WriteOnly attributes are null in state and null in the plan, changing `client_secret_wo` alone produces **no diff at all** - nothing triggers an Update. That is the same as `datahub_secret`; the `*_wo_version` counter is the standard workaround.

But there is a second, non-obvious consequence specific to *this* resource. Since `client_secret_wo` is available in `req.Config` on **every** apply, the naive implementation sends it on every Update - and because every non-empty write mints a new secret entity, that orphans a secret on every unrelated update (a description edit, a scope change). So:

> **Update includes `clientSecret` in the mutation input only when `plan.client_secret_wo_version != state.client_secret_wo_version`. Otherwise it sends `null`, which preserves the stored secret.**

On a version bump, the value written is whatever `client_secret_wo` holds in config - and a **null** `client_secret_wo` with a bumped version writes the empty string, which clears the secret. That gives a coherent rule: the version says "act on the secret now", the attribute says what to act with.

Create always sends the configured value if present.

### Consequences for Read, drift and import - honestly

**Read** can never populate `client_secret_wo`. It populates `has_client_secret` and `client_secret_urn` from the server and leaves the WriteOnly attribute null. This is not a degraded Read: it is complete for everything except one field whose value is unobtainable by design.

**Drift detection is partial and asymmetric.**

- Every non-secret field (`display_name`, `description`, `client_id`, `authorization_url`, `token_url`, `scopes`, `token_auth_method`, `auth_location`, `auth_header_name`, `auth_scheme`, `auth_query_param`, the additional-params maps) is fully drift-detected via the strongly-consistent read.
- **Secret presence** is drift-detected: if an admin clears the secret in the UI, `has_client_secret` flips to `false` and the plan shows it. Terraform will not automatically re-push the secret (no diff on the WriteOnly attribute), so the correct operator response is to bump the version. The provider should say so in the diagnostic-adjacent doc text rather than leaving the user with an unactionable computed-attribute diff.
- **Secret value drift is undetectable.** If an admin rotates the client secret in the UI to a different value, Terraform sees `has_client_secret = true` before and after and reports no drift. The config's value is stale and Terraform will not correct it until the version is bumped. State this plainly; do not imply otherwise.
- `client_secret_urn` changing between reads is a *signal* that someone rotated the secret out-of-band. Tempting to treat as drift - but it also changes on every legitimate Terraform rotation, so it would produce a diff the provider itself caused. Keep it Computed and informational; do not derive drift from it.

**Import works better here than for `datahub_secret`, and that is the surprising part.** `datahub_secret`'s docs say an imported secret cannot be updated until you supply `value`, because its update mutation requires the value on every call. This mutation does not: `null` preserves. So an imported `datahub_oauth_authorization_server` can be renamed, re-scoped, re-pointed at new URLs, and re-applied **without ever re-supplying the secret** - leave `client_secret_wo` and `client_secret_wo_version` both unset and the stored secret is untouched. `has_client_secret = true` after import tells the user the secret is there.

The corollary: because a null version never triggers secret handling, a user who imports and *then* wants to rotate must set both the value and the version in the same change. Bumping the version from null to `1` is a legitimate change and works.

### The `client_id` decision

The demo keeps `clientId` out of state alongside the secret. This design **does not**, and the divergence is deliberate.

`clientId` is not a secret - the fork's own schema comments it "public, safe to expose" - and the server returns it on read. Making it WriteOnly would forfeit drift detection on a readable, non-secret field in exchange for nothing. The demo's motivation is convenience, not confidentiality: both values arrive from the same live Snowflake call, so the script naturally treats them alike.

Terraform can still avoid hard-coding it - `client_id = data.external.snowflake_oauth.result.client_id` - it just means the value lands in state, which is appropriate for a non-secret. It is marked `Sensitive` so it does not appear in plan output.

If a customer objects (some organisations treat OAuth client ids as semi-confidential), the additive follow-up is a `client_id_wo` alternative alongside `client_id`, mutually exclusive at plan time - the pattern several providers use for `password`/`password_wo`. Not built now; recorded so the option is not lost. **This is a genuine judgement call**: it is the one place this design consciously departs from the demand signal.

### `SHARED_API_KEY` and why it is deferred

`SHARED_API_KEY` would add a second WriteOnly secret (`shared_api_key_wo`) with a *different* server-side mechanism: the upsert creates a `dataHubConnection` holding the encrypted key, with an id derived by sanitising the service URN (`urn_li_service_<id>__apiKey`). Two consequences worth recording even though we are not building it:

- That connection is invisible to `datahub_connection`, and correctly so: the existing connection enumerator already filters out ids beginning `urn_li_` precisely because Cloud creates internal OAuth/system connections in that namespace. It would need no change.
- `deleteAiPlugin` deletes that connection as part of its cascade.

Deferred because the demand signal does not need it and it doubles the WriteOnly surface. `USER_API_KEY` carries no server-side secret at all (users supply their own keys later) and is only four injection fields, so it is the cheaper follow-up.

**Phase 1 auth types: `NONE` and `USER_OAUTH`.** The schema shape - a flat `auth_type` plus a single-nested `oauth {}` block - makes `user_api_key {}` and `shared_api_key {}` purely additive later.

## Read path

Both resources read only strongly-consistent endpoints. No `list*` query on any Read or ImportState path.

**`datahub_oauth_authorization_server`:** `GET /openapi/v3/entity/oauthauthorizationserver/{urn}` -> `oauthAuthorizationServerKey` + `oauthAuthorizationServerProperties`.

**`datahub_ai_plugin`: two reads, both OpenAPI v3.**

1. `GET /openapi/v3/entity/service/{urn}` -> `serviceKey`, `serviceProperties`, `subTypes`, `mcpServerProperties`, and `globalTags` when the defaults latch is engaged.
2. `GET /openapi/v3/entity/globalsettings/urn:li:globalSettings:0` -> `globalSettingsInfo.value.aiPluginSettings.plugins[]`, then select the entry whose `serviceUrn` equals ours -> `enabled`, `instructions`, `authType`, `oauthConfig.serverUrn`, `oauthConfig.requiredScopes`.

Read (2) reuses the client machinery `datahub_organization_display_preferences` already established, including the empirically-confirmed all-lowercase `globalsettings` path segment and the working v3 read on the singleton. That is the single most reassuring existing datapoint in this design: the harder of the two reads is already proven on a live Cloud instance.

**Why the v3 endpoints should resolve for these types.** The v3 entity controller is generic over the entity registry - one `@RequestMapping("/openapi/v3/entity")` with an `{entityName}` path variable resolved through `entityRegistry.getEntitySpec(entityName)`, which lower-cases its argument before lookup. That is the same mechanism that makes `globalsettings` work for a registry entry named `globalSettings`. So registration is *structural*, not per-type. Two risks survive it, and both need a live probe: whether the deployed Cloud build's registry contains these entries, and whether the per-entity-type API read authorization grants for them (neither entity carries `viewUnrestricted: true`). See Open question 2 - the roadmap's "unverified" flag is narrowed, not cleared.

**Read-back verification after write**, per the project's silent-no-op guard.

**Guards on Read and ImportState for `datahub_ai_plugin`.** The `service` entity type is shared: the fork's Agent Registry can auto-register services from LangChain/ADK, and a future pure-`Service` API is explicitly reserved. So the resource must refuse to manage a `service` that is not an AI plugin, in the same spirit as `datahub_service_account`'s `SERVICE_ACCOUNT` subtype check:

- **No matching entry in `aiPluginSettings.plugins`** -> on Read, `RemoveResource` (the plugin registration is gone even though the entity survives - which is exactly the state a partially-failed upsert leaves behind). On ImportState, a clear error: this service exists but is not registered as an AI plugin.
- **`subTypes` is not the MCP value** -> refuse on import.

The first guard has a subtlety. The upsert writes the entity aspects *before* the singleton entry and the fork's resolver documents the failure mode: the service can exist while the plugin registration does not, recoverable by retrying the upsert. So "entity present, singleton entry absent" is a reachable state, and `RemoveResource` is the right response - the next apply re-upserts and repairs it.

## Concurrency: the singleton race

`upsertAiPlugin` and `deleteAiPlugin` both perform an **unlocked read-modify-write** of the `globalSettingsInfo` aspect. The fork says so in its own comments, describes the lost-update window, and defers proper locking to a later phase pending Redis. `deleteOAuthAuthorizationServer` does the same when rewriting referencing plugins.

Terraform applies with `-parallelism=10` by default. Three `datahub_ai_plugin` resources in one config are a textbook trigger: three concurrent read-modify-writes of one aspect, and the losers vanish.

**Mitigation: serialise these calls in the client behind the existing `keyedMutex`, keyed on `urn:li:globalSettings:0`.** The primitive already exists (`pkg/datahub/keyedmutex.go`, added for the structured-property CAT-2568 workaround) and this is exactly its intended use. Wire it around `UpsertAiPlugin`, `DeleteAiPlugin`, and `DeleteOAuthAuthorizationServer`, and around any future `datahub_ai_settings` write.

Be honest about the bound: an in-process mutex protects one `terraform apply` in one provider instance. It does **not** protect against a second concurrent apply, a concurrent UI edit, or a concurrent SDK script. Document that managing AI plugins from one place is the operating assumption, the same guidance `datahub_organization_display_preferences` gives for its singleton.

The mutex is worth wiring regardless of what the probe shows, because the server source says the race is real and the cost is a few lines. The probe's value is a regression test, not the decision.

## Cross-resource coupling

Recording one constraint now so it is not discovered later:

**A future `datahub_ai_settings` must not write `globalSettingsInfo` directly via OpenAPI v3.** The aspect is a single blob, and a direct v3 aspect write with a partial body would clobber `aiPluginSettings.plugins`, silently deleting every AI plugin registration in the tenant. It must go through `updateGlobalSettings` (or a narrower mutation), which read-modify-writes server-side, exactly as `datahub_organization_display_preferences` does. This is the same "write via GraphQL, read via OpenAPI v3" rule the project already states, but the consequence of breaking it here is unusually destructive, so it belongs in the roadmap row too.

## OSS vs Cloud availability

**Both resources are Cloud-only.** Verdict per the roadmap's OSS verification checklist:

| Check | `oauthAuthorizationServer` | `service` / AI plugin |
|---|---|---|
| Mutation in OSS `datahub-graphql-core/src/main/resources/`? | No | No |
| Entity in OSS `entity-registry.yml`? | No | **Yes** (`category: core`) |
| Key PDL in OSS? | No | **Yes** (`ServiceKey.pdl`) |
| `AiPluginConfig` / `AiPluginSettings` PDL in OSS? | n/a | No - fork only |
| OSS Python SDK URN class? | No | **Yes** (`ServiceUrn`) |
| Badge | `cloudOnlyBadge` | `cloudOnlyBadge` |

The roadmap's Category-11 note lists `aiPlugin` as "Cloud-experimental: file only in fork, no OSS equivalent". That needs qualifying: the *GraphQL surface* is fork-only, but the `service` entity type has been upstreamed to OSS as `category: core`, and its URN class ships in the OSS SDK. `oauthAuthorizationServer` really is fork-only end to end. Both resources are still Cloud-only in practice, because neither mutation exists on OSS - but the entity model is no longer purely fork-side, which matters for anyone reasoning about future OSS availability.

**Stability posture.** Both entity types are `category: core`, not `internal`. But the plugin's configuration half lives on `globalSettings`, which *is* `category: internal`, and the whole write surface is fork-only. Weakest link governs.

Per the project's established convention (recorded in `provider-org-settings.md`), that reality stays in **maintainer-facing** docs - this file, `CLAUDE.md`, the roadmap. User-facing docs get `cloudOnlyBadge` plus the **ownership-style note**, not an "experimental / no stability guarantee" disclaimer. Reuse the `datahub_organization_display_preferences` wording: this is a DataHub Cloud capability, Cloud upgrades on its own cadence so a release may occasionally affect the resource, fixes are handled in the provider, pin the provider version for client-side stability and upgrade to pick up fixes, please open an issue.

There is real evidence for that note being warranted rather than boilerplate: the `ServiceSubType` enum value was renamed from `MCP_SERVER` to `MCP` in fork main on 2026-07-16 - a breaking rename in the exact field the resource must send on every write. See Open question 1.

**Exactly which docs and tables need entries:**

| Location | Change |
|---|---|
| `internal/provider/availability.go` | none - reuse `cloudOnlyBadge` |
| `CLAUDE.md` "Cloud-only resources" table | two new rows, worded like the `datahub_organization_display_preferences` row (name the fork-only mutations, note `globalSettings` is `category: internal`, note OSS contributors cannot see the backing source, link this doc) |
| `CLAUDE.md` "Resources with OSS vs Cloud behavioral differences" | no entry - there is no OSS behaviour to differ from |
| `docs/roadmap.md` | see "Roadmap edits" |
| `docs/guides/provider-defaults.md` support matrix | add `datahub_ai_plugin` to the tags row; add `datahub_oauth_authorization_server` to the "everything else / documented no-op" row |
| `internal/provider/provider.go` `defaults.tags` description | add `datahub_ai_plugin` |
| `docs/index.md` | regenerated (picks up the `provider.go` change) |
| `docs/resources/ai_plugin.md`, `docs/resources/oauth_authorization_server.md` | generated |
| `templates/resources/ai_plugin.md.tmpl` | recommended - the cascade-delete and singleton-race guidance is long-form prose, the same reason `connection`/`secret`/`ingestion_source`/`remote_executor_pool` have templates |
| `CHANGELOG.md` | one Added entry in `## [Unreleased]` covering both, inline links only, per the goreleaser awk constraints (the version number is assigned at release time) |

## Open questions requiring live probing

Ordered by how much a different answer changes the design. Nothing here was probed - the planning pass had no live access.

### 1. Which `ServiceSubType` literal does the target accept - `MCP` or `MCP_SERVER`? (highest impact)

The fork renamed `ServiceSubType.MCP_SERVER` to `MCP` on **2026-07-16** (the Agent Registry change), on the reasoning that "Service" already conveys "server". Fork main validates `subType` against `MCP` and rejects anything else. The demo script sends `"MCP_SERVER"` and `DEMO-AI.md` records it deploying successfully on **2026-07-30** - so demo.acryl.io is running a Cloud build cut before the rename. Two live tenants can therefore disagree about a required enum value.

Compounding it: `AiPluginType.MCP_SERVER` (the *other* enum, on the singleton config) still is and always was `MCP_SERVER`. So `MCP_SERVER` is simultaneously wrong for `subType` on new builds and correct for the config type on all builds. Easy to conflate.

**Probe.** Against demo.acryl.io, issue `upsertAiPlugin` with `subType: MCP` on a throwaway id. A graphql-java enum-validation error means the tenant is pre-rename. Repeat with `MCP_SERVER`. GraphQL introspection is not an option - the roadmap records that live introspection is gated on Cloud by a bad-faith-introspection guard.

**Impact if the answer is "it depends on the build".** The client needs a negotiate-and-cache step: attempt the preferred literal, and on an enum-validation error retry with the legacy one, caching the winner for the process lifetime. There is precedent for a version-shaped fallback (`datahub_connection`'s OSS delete falls back to the OpenAPI endpoint when the mutation is absent). It is ugly and it is testable, and the alternative - a hard version floor with a clear diagnostic - leaves the Woodside tenant unable to use the resource.

**Impact if both tenants of interest accept the same literal.** Send it directly, no negotiation, and note the version floor in the docs.

### 2. Do the OpenAPI v3 entity endpoints resolve for `oauthauthorizationserver` and `service`?

The roadmap flags v3 registration for `oauthauthorizationserver` as unverified. The structural reading narrows the risk considerably - the v3 controller is generic over the registry and lower-cases the entity name for lookup, the same mechanism proven live for `globalsettings` - but two things remain unproven: the deployed build's registry contents, and the per-entity-type API read authorization (`isAPIAuthorizedEntityType(READ, ...)`); neither entity carries `viewUnrestricted: true`.

**Probe.**

```bash
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
  "$DATAHUB_GMS_URL/openapi/v3/entity/service/urn:li:service:wds-snowflake-mcp"

curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "Authorization: Bearer $DATAHUB_GMS_TOKEN" \
  "$DATAHUB_GMS_URL/openapi/v3/entity/oauthauthorizationserver/urn:li:oauthAuthorizationServer:wds-snowflake-oauth"
```

Expect `200` with the key and properties aspects. Both URNs exist on demo.acryl.io per `DEMO-AI.md`, so this is a cheap probe against real data. `service` is the lower-risk of the two (it is in the OSS registry as well). Also confirm a `404` - not a `500` - for a nonexistent URN, since Read depends on distinguishing them.

**Impact if it 404s or 403s.** Read and ImportState would fall back to the fork's `oauthAuthorizationServer(urn)` / `service(urn)` GraphQL single-entity queries. Those are point reads by URN, not `list*` - served from the primary datastore, so the eventual-consistency trap does not apply and the resource would still be correct. But it breaks the letter of the project's "always OpenAPI v3 for Read" rule, so the rule would need a documented exception with the reasoning, and the roadmap's "Read path per resource" section would need the carve-out. **It would not block the design.** For the plugin it would matter less: read (2), the singleton read, is the one that is already proven, and it is the one carrying the fields most likely to drift.

### 3. Can `required_scopes` be cleared?

The upsert resolver overwrites `requiredScopes` only when the input list is non-null **and non-empty**; an empty list falls through to preserving the stored value. So `required_scopes = []` is probably a silent no-op, which breaks full-list ownership for that one field. Contrast the OAuth server's own `scopes`, where an empty array *is* written and does clear.

**Probe.** Apply with `required_scopes = ["session:role:X"]`; read the singleton via OpenAPI v3 and confirm it is stored. Apply again with `required_scopes = []`; read again. If the value survives, it is unclearable through this mutation.

**Impact.** If unclearable: document it as a one-way field, and **reject `[]` at plan time** with a diagnostic explaining the situation, rather than accepting a config the provider cannot honour and letting the user watch a diff that never converges. (Accepting-and-no-op'ing would produce a permanent plan diff, which is worse than a clear error.) Also note the workaround: switching `auth_type` away from `USER_OAUTH` and back drops `oauthConfig` entirely.

### 4. Is the singleton read-modify-write race observable from Terraform?

**Probe.** One config with three `datahub_ai_plugin` resources, default parallelism, `terraform apply`. Then read `globalSettingsInfo.aiPluginSettings.plugins` via OpenAPI v3 and count entries. Repeat with `-parallelism=1` as a control. Repeat for concurrent destroys.

**Impact.** If entries are lost, the `keyedMutex` is mandatory and the test becomes a permanent regression guard. If not observable, wire the mutex anyway (the server source documents the race; absence of observation is not absence of race) and keep the test as a canary. Either way the docs carry the "manage AI plugins from one place" guidance.

### 5. Do the cascades behave as the source says?

Three sub-probes, all cheap once the resources exist, and all of them shaping user-facing documentation.

- Apply server + plugin; `terraform destroy -target=datahub_ai_plugin.x`; then GET the server URN. Expect gone (unshared server cascade-deleted).
- Same setup with **two** plugins sharing one server; destroy one; GET the server. Expect present (shared server preserved).
- Apply server + plugin; `terraform destroy -target=datahub_oauth_authorization_server.x`; then read the singleton. Expect the plugin's `authType` flipped to `NONE` and `oauthConfig` gone. Then `terraform plan` and confirm the provider reports it as drift on the plugin.

**Impact.** The design already accommodates all three (404-tolerant Read, not-found-tolerant Delete). What the probes settle is the *documentation*, which is where users get hurt, and whether the drift is reported cleanly or produces a confusing plan.

### 6. Hard delete or soft delete?

Read as hard (`entityService.deleteUrn`), but both entities register `status`, so the assumption is worth checking.

**Probe.** Destroy, then GET the URN on OpenAPI v3. Expect `404`, not `200` with `status.removed = true`. Then re-apply the same id and confirm no stale aspects reappear (e.g. a description from the previous incarnation that the new config does not set).

**Impact.** If it turns out to be soft: the resurrection caveat from the model doc applies, the docs need it, and a `force_hard_delete` escape hatch becomes worth considering. As read, none of that is needed.

### 7. `timeout` semantics

GraphQL types it `Float`; a fork commit made Python the single source of truth for the default; the aspect stores a float and null means "server applies its own default".

**Probe.** Set `timeout = 30`; read back and confirm the stored form (`30.0` vs `30`). Then omit it entirely and confirm the field is *absent* rather than populated with a server default.

**Impact.** If the server materialises a default into the aspect, a plain Optional attribute drifts `null -> 30.0` on every plan, and the attribute needs Optional+Computed with `UseStateForUnknown`. Small, but it is exactly the kind of thing that produces a permanent diff nobody can explain.

### 8. Does `globalTags` actually accept a write on `service` in the deployed build?

The defaults answer above is derived from the registry at fork main.

**Probe.** With `defaults.tags` set, apply a `datahub_ai_plugin`, then `GET /openapi/v3/entity/service/{urn}` and confirm a `globalTags` aspect came back with the tag.

**Impact.** If rejected or silently dropped, `kindService` becomes `{Tags: false}` and the guide's matrix moves `datahub_ai_plugin` to the no-op row. This is the checklist's designated silent-failure item, so it must be asserted by a test, not assumed.

### 9. Privilege and diagnostics

Both mutations gate on `MANAGE_CONNECTIONS`. **Probe** with a PAT lacking it and confirm the provider surfaces a legible diagnostic rather than a raw GraphQL authorization blob. Low design impact; the privilege name belongs in the resource docs either way.

### What could not be verified at all

- **Anything live.** No probe in this list was run. Every server-behaviour claim is source-derived from `datahub-fork@fdb870497a9` and `datahub@f4bc6383ff`, and the deployed Cloud build is neither.
- **Whether the demo script actually succeeds as written.** `DEMO-AI.md` says it deployed on 2026-07-30, but that conflicts with the `subType` enum in fork main (Open question 1). One of the two is stale and it is not determinable from source alone.
- **Whether `listServices` / `listOAuthAuthorizationServers` are reliable enough for enumeration.** The roadmap's live-probe list confirms `listOAuthAuthorizationServers` *exists* on the user's Cloud instance; it says nothing about lag or completeness.
- **The end-to-end user experience.** Whether Ask DataHub actually invokes the MCP tools after a Terraform-registered plugin is connected is a human-in-the-loop check, not a provider test.

### What the demo left ambiguous

Because it is in-flight, three things in the demo are evidence-of-intent rather than specification:

1. **`subType: "MCP_SERVER"`** - contradicts fork main (Open question 1). This is the one that could break the resource on day one.
2. **Scopes ride the OAuth server, not the plugin.** The script sets `scopes: ["refresh_token", "session:role:WDS_DEMO_READER"]` on the server and never sets the plugin's `requiredScopes`. That is a legitimate choice (the server's base scopes are merged with the plugin's required scopes when a user connects), but it means **the demo exercises neither `required_scopes` nor any auth-injection field**. Those parts of the resource schema have no field evidence behind them and should be treated as inference from the API, not as validated requirements.
3. **`clientId` treatment.** The demo keeps it out of state; this design does not. Deliberate, argued above, and the point where user confirmation is most wanted.

## Implementation outline

Two PRs on `feat/ai-plugin`, released together.

### PR 1 - `datahub_oauth_authorization_server`

**New files**

| File | Contents |
|---|---|
| `internal/provider/pkg/datahub/oauth_authorization_servers.go` | `UpsertOAuthAuthorizationServer`, `GetOAuthAuthorizationServerByURN` (OpenAPI v3), `DeleteOAuthAuthorizationServer`, `ListOAuthAuthorizationServerURNs`; `ErrOAuthAuthorizationServerCloudOnly` + the two-code `FieldUndefined`/`UnknownType` schema-absence detector, modelled on `organization_display_preferences.go` (matching only `FieldUndefined` is the documented trap); URN prefix and lowercase entity-path constants; the `keyedMutex` wrap on Delete |
| `internal/provider/pkg/datahub/oauth_authorization_servers_test.go` | request-shape and response-decode unit tests, including one per null-semantics field from the table above, and one asserting the secret is omitted when the version is unchanged |
| `internal/provider/oauth_authorization_server_resource.go` | the resource |
| `internal/provider/oauth_authorization_server_resource_test.go` | mock acceptance |
| `internal/provider/oauth_authorization_server_data_source.go` | singular data source (no secret exposure; `has_client_secret` only) |
| `internal/provider/oauth_authorization_servers_data_source.go` | bulk enumerate |
| `internal/provider/datahubtesting/oauth_authorization_servers.go` | mock GraphQL mutation + OpenAPI v3 handlers |
| `internal/provider/datahubtesting/oauth_authorization_server_scenarios.go` | reusable lifecycle/import/rotation/OSS-rejection step builders |
| `examples/resources/datahub_oauth_authorization_server/resource.tf`, `import.sh` | registry doc snippets, `tf-example-` id convention |

**Modified**

- `internal/provider/provider.go` - register resource + two data sources
- `internal/provider/importtarget/targets/targets.go` - registration (enumerable; `OSSCompatible: false`)
- `CLAUDE.md` - Cloud-only table row
- `docs/guides/provider-defaults.md` - add to the no-op row
- `docs/roadmap.md` - see below
- generated: `docs/resources/oauth_authorization_server.md`, `docs/data-sources/*`

**Schema sketch.** `id` (Required, RequiresReplace, validated), `urn`/`id`-computed pair per convention, `display_name` (Required), `description`, `client_id` (Optional, Sensitive), `client_secret_wo` (Optional, WriteOnly, Sensitive), `client_secret_wo_version` (Optional Int64), `has_client_secret` (Computed Bool), `client_secret_urn` (Computed), `authorization_url`, `token_url`, `scopes` (Optional list), `token_auth_method` (Optional, `enumString("BASIC","POST_BODY","NONE","CUSTOM")`, default `POST_BODY`), `auth_location` (`HEADER`/`QUERY_PARAM`, default `HEADER`), `auth_header_name` (default `Authorization`), `auth_scheme`, `auth_query_param`, `additional_token_params` / `additional_auth_params` (Optional maps, `nonEmptyStringMapValidator`).

### PR 2 - `datahub_ai_plugin`

**New files**

| File | Contents |
|---|---|
| `internal/provider/pkg/datahub/ai_plugins.go` | `UpsertAiPlugin` (with the subType-literal negotiation and the `keyedMutex` on the singleton), `GetAiPluginByURN` (the two-read assembly), `DeleteAiPlugin`, `ListAiPluginServiceURNs` (from the singleton, not `listServices`); `ErrAiPluginCloudOnly` + detector |
| `internal/provider/pkg/datahub/ai_plugins_test.go` | two-read assembly, missing-singleton-entry handling, subType negotiation, guard logic |
| `internal/provider/ai_plugin_resource.go` | the resource, including `ModifyPlan` -> `planTagsAll` |
| `internal/provider/ai_plugin_resource_test.go` | mock acceptance incl. default-tags wiring, plugin-entry guard, subtype guard |
| `internal/provider/ai_plugin_data_source.go`, `ai_plugins_data_source.go` | data sources |
| `internal/provider/datahubtesting/ai_plugins.go`, `ai_plugin_scenarios.go` | mock handlers + step builders |
| `examples/resources/datahub_ai_plugin/resource.tf`, `import.sh` | snippets |
| `examples/runnable/ai-plugin-mcp-oauth/` | `main.tf`, `variables.tf`, `outputs.tf`, `README.md` - both resources wired together, `required_version = ">= 1.11"`, `tf-example-` ids with a per-directory slug (e.g. `tf-example-aiplugin-*`; `example_identifier_test.go` fails on any identifier two directories share, and the new resource types must be classified in its `urnKeyedResources`), `TF Example - ` display names, secret via a sensitive variable, outputs exposing both URNs plus a copy-pasteable verification command and the `$DATAHUB_GMS_URL/settings/ai` navigation path. **Post-design obligation (stage C, PR #128, 2026-08-12):** the directory must also be classified in `internal/provider/example_live_classification_test.go` - as a `permanent: true` entry in `liveExampleExclusions` (both resources are Cloud-only; the live harness targets an OSS Quickstart) - or `TestEveryRunnableExampleIsClassified` fails the build. |
| `templates/resources/ai_plugin.md.tmpl` | long-form cascade/singleton-race guidance |

**Modified**

- `internal/provider/provider.go` - register; add `datahub_ai_plugin` to the `defaults.tags` description
- `internal/provider/defaults.go` - `kindService` in `entityKind` + `allEntityKinds`, a `defaultsSupport` row, the kind-to-entity-path entry
- `internal/provider/importtarget/targets/targets.go` - registration
- `CLAUDE.md` - Cloud-only table row
- `docs/guides/provider-defaults.md` - tags row
- `docs/roadmap.md` - see below
- `CHANGELOG.md` - one `## [0.18.0]` entry covering both resources
- generated: `docs/index.md`, `docs/resources/ai_plugin.md`, `docs/data-sources/*`

**Schema sketch.** `id` (Required, RequiresReplace, validated), `urn`/`id`, `display_name` (Required), `description`, `enabled` (Optional Bool, default true), `instructions` (Optional String), `mcp_server` single-nested block (`url` Required, `transport` Optional `HTTP`/`SSE`/`WEBSOCKET` default `HTTP`, `timeout` Optional Float64, `custom_headers` Optional map), `auth_type` (Optional, `enumString("NONE","USER_OAUTH")` in phase 1), `oauth` single-nested block (`server_urn` Required, `required_scopes` Optional list), `tags_all` (Computed).

Plan-time validators: `oauth` required iff `auth_type = USER_OAUTH` and forbidden otherwise; `mcp_server` required (the only subtype today); `required_scopes = []` rejected pending Open question 3.

The two single-nested blocks put this resource squarely under the "Nested attributes and unknown values" rules in `CLAUDE.md` (this class of bug has now shipped three times): model `mcp_server` and `oauth` as `types.Object`, never `*fooModel`; check `IsUnknown()` explicitly in every `ValidateConfig`/`ModifyPlan` presence test; and add at least one non-literal test (`ConfigVariables` on the `TestStep`, or feed `oauth.server_urn` from `datahub_oauth_authorization_server.x.urn` - the natural wiring already exercises it, but only if the test config actually uses the reference rather than a literal URN string).

**Cross-resource guards.** OAuth server `Read` treats 404 as gone and `RemoveResource`; `Delete` treats not-found as success. Both behaviours are needed because of the plugin's cascade and both need mock coverage.

### Docs regeneration

`cd tools && go generate ./...`, then `make lint` (includes `gofmt` - comment alignment fails CI on its own).

## Verification plan

### Provable in CI (`make test` - unit + mock acceptance)

- URN construction; id validation against the reserved-character set; rejection of a `urn:li:`-prefixed id
- GraphQL input marshalling: every field present on every write; `newOAuthServer` **never** present; explicit `id` always present
- The rotation rule: `clientSecret` in the input **iff** the version changed; a bumped version with a null value marshals the empty string
- WriteOnly nullification: `client_secret_wo` absent from state after create, update and import
- Two-read assembly for the plugin; the "entity present, singleton entry absent" path removing the resource
- The subtype guard and the not-a-plugin import error
- `subType` literal negotiation and caching
- Full lifecycle + import + drift for both resources against the mock
- The Cloud-only diagnostic for both (asserting the provider's message, not a raw GraphQL error)
- Default-tags wiring on `datahub_ai_plugin`, including tag-verification-before-write
- `TestImportTargetCoverage` and the defaults matrix-completeness test

### Provable only against live DataHub Cloud (`make testacc`)

Everything in "Open questions", plus:

- Both cascade directions, including the shared-server case
- The singleton read-modify-write race, with `-parallelism=1` as a control
- Hard-vs-soft delete, and recreate-after-destroy leaving no stale aspects
- `has_client_secret` drift after an out-of-band clear
- The rotation orphan trail (assert `client_secret_urn` changes; assert the previous secret URN still resolves - i.e. confirm the orphan, so the documentation is true)
- `required_scopes` clearability
- `timeout` round-trip and absent-when-omitted
- `globalTags` acceptance on `service`
- The `MANAGE_CONNECTIONS` diagnostic with an under-privileged PAT

**Be explicit about the limit: the mock can only prove what the provider *sends*. Only live Cloud proves the server does what the fork source says it does.** Every cascade, every preserve-on-null, and the whole singleton race fall in that category. Cascades in particular cannot be modelled convincingly in the mock without reimplementing the resolver's logic, which would only prove the mock agrees with itself.

### Not provable against OSS at all

Both resources fail on OSS by design. The nightly Quickstart job asserts the Cloud-only diagnostic, matching how `TestAcc_OrganizationDisplayPreferences_OSS_RejectsWithCloudOnlyError` works.

### Not provable by the provider

The per-user "Connect" OAuth handshake (a human clicks it, Snowflake redirects, a per-user grant is minted) and whether Ask DataHub subsequently invokes the MCP tools. Manual, and worth doing once against the Woodside estate after the resources land, since that is the only end-to-end proof that a Terraform-registered plugin is functionally equivalent to a script-registered one.

## Roadmap edits

Nine edits, applied to `docs/roadmap.md` alongside this document. Summary: promote `upsertAiPlugin`/`deleteAiPlugin` out of Category 11's IRRELEVANT bucket into a new Tier 3 item; record the hard dependency on item 15 in both directions; correct the Cloud-experimental claim about `aiPlugin` (the `service` entity is in OSS; the mutations are not); add both URN rows and both read-path entries; record the `datahub_ai_settings` constraint; note the per-field null-semantics caveat in the aspect-list-ownership section.

The substantive one is the Category 11 promotion. Category 11's justification is "all Cloud-only, runtime/per-user, or experimental". The AI plugin registry is none of those three: it is tenant-level, admin-owned, slow-moving configuration gated by a single privilege - the same profile as `datahub_connection`, which the roadmap rates HIGH. The genuinely per-user siblings (`updateUserAiPluginSettings`, the Connect flow) do belong in that bucket and stay there. `upsertAiPlugin` was grouped with them by adjacency: it lives in the same fork schema neighbourhood as the AI-assistant runtime, not because it shares their shape.

Separately noted and deliberately not edited, to keep the diff focused: the roadmap's "current provider state" paragraph near the top is badly stale - it omits roughly fifteen shipped resources including all six assertion resources, `datahub_service_account`, and `datahub_organization_display_preferences`. Worth a separate housekeeping pass.
