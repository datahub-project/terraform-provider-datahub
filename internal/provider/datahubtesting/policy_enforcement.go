// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// This file holds the live-target machinery for proving that a policy actually
// grants and denies: probes issued AS another user, via a minted token whose
// identity is verified before any assertion is allowed to count. Everything
// here is enforcement-side; CRUD fidelity for the same resources lives in
// scenarios.go.
//
// The probes speak GraphQL, deliberately. GraphQL resolvers enforce privileges
// unconditionally, whereas REST/OpenAPI authorization is deployment-config
// dependent (REST_API_AUTHORIZATION_ENABLED), so a REST probe could pass or
// fail for reasons that have nothing to do with the policy under test.

// GraphQLResult carries the outcome of one GraphQL call: the raw data payload
// and any GraphQL-level error messages. Policy denials arrive as HTTP 200 plus
// an errors array, so both fields are needed to tell "allowed", "denied" and
// "broken" apart.
type GraphQLResult struct {
	Data   json.RawMessage
	Errors []string
}

// Denied reports whether the call failed at all (any GraphQL error). Use
// Unauthorized to additionally check the failure was an authorization denial
// rather than some unrelated error.
func (r GraphQLResult) Denied() bool {
	return len(r.Errors) > 0
}

// Unauthorized reports whether any GraphQL error message reads as an
// authorization denial. A probe that fails for a different reason (bad URN,
// server fault) must not be miscounted as the policy working.
func (r GraphQLResult) Unauthorized() bool {
	for _, msg := range r.Errors {
		if strings.Contains(strings.ToLower(msg), "unauthorized") {
			return true
		}
	}
	return false
}

// UserPrincipal is how an enforcement probe authenticates as a specific actor.
// Exactly one of the two fields is set. token is the normal path: a personal
// access token the server resolves to the actor. actorHeader is the
// Quickstart-only fallback: METADATA_SERVICE_AUTH_ENABLED=false makes GMS
// ignore Bearer tokens entirely (every request resolves to
// urn:li:corpuser:__datahub_system), and the X-DataHub-Actor header is then
// the only way to be someone -- the same mechanism scripts/quickstart-token.sh
// uses to mint the suite's own admin PAT. AsUser picks the path and verifies
// it; nothing else constructs a non-admin principal.
type UserPrincipal struct {
	token       string
	actorHeader string
}

// adminPrincipal returns the environment's admin credentials as a principal.
func adminPrincipal() UserPrincipal {
	return UserPrincipal{token: os.Getenv("DATAHUB_GMS_TOKEN")}
}

// graphQLAs posts one GraphQL request to GMS as the given principal and
// returns the data payload plus GraphQL-level errors. Transport failures and
// HTTP-level auth rejections are fatal: a policy denial is an HTTP 200 with
// an errors array, so an HTTP 401/403 means the credential itself is broken,
// which is a fixture problem, never a verdict.
func graphQLAs(t *testing.T, as UserPrincipal, query string, variables map[string]any) GraphQLResult {
	t.Helper()

	gmsURL := strings.TrimRight(os.Getenv("DATAHUB_GMS_URL"), "/")
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		t.Fatalf("graphQLAs: marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		gmsURL+"/api/graphql", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("graphQLAs: build request: %v", err)
	}
	if as.token != "" {
		req.Header.Set("Authorization", "Bearer "+as.token)
	}
	if as.actorHeader != "" {
		req.Header.Set("X-DataHub-Actor", as.actorHeader)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("graphQLAs: POST /api/graphql: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		t.Fatalf("graphQLAs: HTTP %d -- the token itself was rejected. A policy denial "+
			"is HTTP 200 with a GraphQL errors array, so this is a broken fixture, not a verdict.",
			resp.StatusCode)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		t.Fatalf("graphQLAs: unexpected HTTP %d from /api/graphql", resp.StatusCode)
	}

	var decoded struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("graphQLAs: decoding response: %v", err)
	}

	out := GraphQLResult{Data: decoded.Data}
	for _, e := range decoded.Errors {
		out.Errors = append(out.Errors, e.Message)
	}
	return out
}

// whoAmI resolves the principal's identity via the `me` query.
func whoAmI(t *testing.T, as UserPrincipal) string {
	t.Helper()
	who := graphQLAs(t, as, `{ me { corpUser { urn } } }`, nil)
	if who.Denied() {
		t.Fatalf("whoAmI: `me` failed: %v", who.Errors)
	}
	var me struct {
		Me struct {
			CorpUser struct {
				Urn string `json:"urn"`
			} `json:"corpUser"`
		} `json:"me"`
	}
	if err := json.Unmarshal(who.Data, &me); err != nil {
		t.Fatalf("whoAmI: decoding `me` payload: %v", err)
	}
	return me.Me.CorpUser.Urn
}

// AsUser builds a principal that provably acts as the given actor, or fails
// the test. Every assertion made through the principal is meaningless unless
// the server resolves it to the intended actor, so the identity is verified
// with a `me` query before the principal is returned -- a check that has
// already caught the real failure mode once: an auth-disabled Quickstart
// ignores Bearer tokens and resolves every request to
// urn:li:corpuser:__datahub_system, which would make every deny assertion
// falsely pass.
//
// Two paths, in order:
//
//  1. Mint a short-lived personal access token for the actor (revoked on
//     cleanup). This is the path a real deployment takes, and the only one
//     that can work where metadata-service auth is enabled.
//  2. Where the minted token does not resolve (the auth-disabled Quickstart),
//     fall back to X-DataHub-Actor header impersonation -- the same mechanism
//     scripts/quickstart-token.sh uses. Only ever reachable on an authless
//     server, because path 1 succeeds everywhere else.
//
// Both paths end at the same who-am-I guard; there is no unverified fallback.
// Live targets only.
func (tg *Target) AsUser(t *testing.T, actorURN string) UserPrincipal {
	t.Helper()
	if tg.Kind == TargetMock {
		t.Fatalf("AsUser: the mock target does not evaluate policies; this helper is live-only")
	}

	const mint = `
mutation createAccessToken($input: CreateAccessTokenInput!) {
  createAccessToken(input: $input) {
    accessToken
    metadata { id }
  }
}`
	res := graphQLAs(t, adminPrincipal(), mint, map[string]any{
		"input": map[string]any{
			"type":        "PERSONAL",
			"actorUrn":    actorURN,
			"duration":    "ONE_HOUR",
			"name":        "tfprovider-enforcement-" + tg.Name("probe"),
			"description": "Short-lived token minted by TestAcc_Policy_DomainScopedEnforcement",
		},
	})
	if res.Denied() {
		t.Fatalf("AsUser: createAccessToken for %s failed: %v (the admin token needs MANAGE_ACCESS_TOKENS)",
			actorURN, res.Errors)
	}

	var payload struct {
		CreateAccessToken struct {
			AccessToken string `json:"accessToken"`
			Metadata    struct {
				ID string `json:"id"`
			} `json:"metadata"`
		} `json:"createAccessToken"`
	}
	if err := json.Unmarshal(res.Data, &payload); err != nil {
		t.Fatalf("AsUser: decoding createAccessToken payload: %v", err)
	}
	minted := payload.CreateAccessToken.AccessToken
	if minted == "" {
		t.Fatalf("AsUser: createAccessToken returned an empty token for %s", actorURN)
	}

	if tokenID := payload.CreateAccessToken.Metadata.ID; tokenID != "" {
		t.Cleanup(func() {
			graphQLAs(t, adminPrincipal(),
				`mutation revoke($id: String!) { revokeAccessToken(tokenId: $id) }`,
				map[string]any{"id": tokenID})
		})
	}

	tokenPrincipal := UserPrincipal{token: minted}
	if got := whoAmI(t, tokenPrincipal); got == actorURN {
		return tokenPrincipal
	} else if got != "urn:li:corpuser:__datahub_system" {
		// A token resolving to some OTHER real user is not the known
		// auth-disabled signature; do not paper over it with the fallback.
		t.Fatalf("AsUser: minted token resolves to %q, wanted %q -- "+
			"probes would run as the wrong principal, so no assertion can be trusted", got, actorURN)
	}

	headerPrincipal := UserPrincipal{actorHeader: actorURN}
	if got := whoAmI(t, headerPrincipal); got != actorURN {
		t.Fatalf("AsUser: token impersonation resolves to __datahub_system (auth-disabled server) "+
			"and X-DataHub-Actor impersonation resolves to %q, wanted %q -- no verified way to act as the steward", got, actorURN)
	}
	t.Logf("AsUser: Bearer tokens are ignored on this server (auth disabled); using verified X-DataHub-Actor impersonation for %s", actorURN)
	return headerPrincipal
}

// AddTagAs applies an existing tag to an entity as the given principal.
// Guarded by EDIT_ENTITY_TAGS on the target entity.
func AddTagAs(t *testing.T, as UserPrincipal, tagURN, resourceURN string) GraphQLResult {
	t.Helper()
	return graphQLAs(t, as,
		`mutation addTag($input: TagAssociationInput!) { addTag(input: $input) }`,
		map[string]any{"input": map[string]any{"tagUrn": tagURN, "resourceUrn": resourceURN}})
}

// CreateTagAs attempts to create a brand-new tag as the given principal. Guarded by the platform-level CREATE_TAGS / MANAGE_TAGS
// privileges, which cannot be domain-scoped -- the probe for "stewards cannot
// mint new vocabulary".
func CreateTagAs(t *testing.T, as UserPrincipal, tagID, name string) GraphQLResult {
	t.Helper()
	return graphQLAs(t, as,
		`mutation createTag($input: CreateTagInput!) { createTag(input: $input) }`,
		map[string]any{"input": map[string]any{"id": tagID, "name": name}})
}

// DatasetReadableAs reports whether the given principal can read the
// dataset entity via GraphQL.
func DatasetReadableAs(t *testing.T, as UserPrincipal, datasetURN string) bool {
	t.Helper()
	res := graphQLAs(t, as,
		`query ds($urn: String!) { dataset(urn: $urn) { urn } }`,
		map[string]any{"urn": datasetURN})
	if res.Denied() {
		return false
	}
	var payload struct {
		Dataset *struct {
			Urn string `json:"urn"`
		} `json:"dataset"`
	}
	if err := json.Unmarshal(res.Data, &payload); err != nil {
		t.Fatalf("DatasetReadableAs: decoding payload: %v", err)
	}
	return payload.Dataset != nil && payload.Dataset.Urn == datasetURN
}

// AwaitPolicyEffect polls the probe until it succeeds (no GraphQL errors).
// GMS caches policies and refreshes on an interval
// (POLICY_CACHE_REFRESH_INTERVAL_SECONDS, default 120s), so a policy created
// moments ago is legitimately not yet in force; a probe that is still denied
// after a full refresh window has genuinely failed. Assertions that expect a
// DENY must run only after this returns, so they are evaluated against the
// same cache generation that admitted the policy.
func AwaitPolicyEffect(t *testing.T, timeout time.Duration, probe func() GraphQLResult) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last GraphQLResult
	for {
		last = probe()
		if !last.Denied() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("AwaitPolicyEffect: probe still failing after %s (policy cache refresh is 120s, so the policy is genuinely not granting): %v",
				timeout, last.Errors)
		}
		time.Sleep(5 * time.Second)
	}
}

// EnsureDatasetEntityInDomain is EnsureDatasetEntity plus a domains aspect, so
// enforcement tests can seed fixtures inside a specific domain. The domain
// entity must already exist: the policy engine resolves the domain hierarchy
// through the graph, and an edge to a nonexistent domain resolves to nothing.
// The dataset is hard-deleted on cleanup. No-op on the mock target.
func (tg *Target) EnsureDatasetEntityInDomain(t *testing.T, entityURN, domainURN string) {
	t.Helper()
	if tg.Kind == TargetMock {
		return
	}

	ensureDataset(t, "EnsureDatasetEntityInDomain", entityURN, map[string]any{
		"datasetProperties": map[string]any{
			"value": map[string]any{"name": "TF Provider Enforcement Fixture"},
		},
		"domains": map[string]any{
			"value": map[string]any{"domains": []string{domainURN}},
		},
	})
}

// PolicyDomainEnforcementSteps builds the two-step enforcement scenario:
//
//	Step 1: the whole cast except the policy -- domains (parent, child,
//	        unrelated), group, steward user, membership, Reader role, tag --
//	        and proves the steward is DENIED an in-domain edit. Without this
//	        negative control the allow assertions in step 2 are worthless: a
//	        steward client that silently acts as admin passes everything.
//	Step 2: the same config plus the domain-scoped METADATA policy, then the
//	        full grant/deny matrix as the steward.
//
// base must already be target-scoped (tg.Name), so repeated runs cannot
// collide on a shared instance.
func PolicyDomainEnforcementSteps(t *testing.T, tg *Target, base string) []resource.TestStep {
	financeID := base + "-finance"
	billingID := base + "-billing"
	marketingID := base + "-marketing"
	groupID := base + "-group"
	tagID := base + "-reviewed"
	policyID := base + "-policy"
	username := base + "@example.invalid"

	tagURN := "urn:li:tag:" + tagID
	financeURN := "urn:li:domain:" + financeID
	billingURN := "urn:li:domain:" + billingID
	marketingURN := "urn:li:domain:" + marketingID

	dsFinance := fmt.Sprintf("urn:li:dataset:(urn:li:dataPlatform:hive,%s.finance,PROD)", base)
	dsBilling := fmt.Sprintf("urn:li:dataset:(urn:li:dataPlatform:hive,%s.billing,PROD)", base)
	dsMarketing := fmt.Sprintf("urn:li:dataset:(urn:li:dataPlatform:hive,%s.marketing,PROD)", base)
	dsOrphan := fmt.Sprintf("urn:li:dataset:(urn:li:dataPlatform:hive,%s.orphan,PROD)", base)

	// No initial_password: the steward acts through a minted token, never a
	// login, so the provider's generated throwaway credential is fine.
	baseConfig := providerBlock + fmt.Sprintf(`
resource "datahub_domain" "finance" {
  domain_id = %q
  name      = "Enforcement Finance"
}

resource "datahub_domain" "billing" {
  domain_id     = %q
  name          = "Enforcement Billing"
  parent_domain = datahub_domain.finance.urn
}

resource "datahub_domain" "marketing" {
  domain_id = %q
  name      = "Enforcement Marketing"
}

resource "datahub_corp_group" "stewards" {
  group_id = %q
  name     = "Enforcement Stewards"
}

resource "datahub_local_user_login" "steward" {
  username  = %q
  email     = %q
  full_name = "Enforcement Steward"
}

resource "datahub_corp_group_member" "steward" {
  group_urn = datahub_corp_group.stewards.urn
  user_urn  = datahub_local_user_login.steward.user_urn
}

data "datahub_role" "reader" {
  name = "Reader"
}

resource "datahub_role_assignment" "steward_reader" {
  actor_urn = datahub_local_user_login.steward.user_urn
  role_urn  = data.datahub_role.reader.urn
}

resource "datahub_tag" "reviewed" {
  tag_id = %q
  name   = "Enforcement Reviewed"
}
`, financeID, billingID, marketingID, groupID, username, username, tagID)

	policyConfig := baseConfig + fmt.Sprintf(`
resource "datahub_policy" "stewards" {
  policy_id   = %q
  name        = "Enforcement Domain Writers"
  type        = "METADATA"
  description = "Curation rights on assets in the finance domain and its sub-domains"

  privileges = [
    "EDIT_ENTITY_OWNERS",
    "EDIT_ENTITY_TAGS",
    "EDIT_ENTITY_GLOSSARY_TERMS",
    "EDIT_ENTITY_DOCS",
  ]

  actors = {
    groups = [datahub_corp_group.stewards.urn]
  }

  resources = {
    filter = {
      criteria = [
        { field = "DOMAIN", values = [datahub_domain.finance.urn] },
      ]
    }
  }
}
`, policyID)

	// Shared across the two steps' check functions.
	var (
		steward  UserPrincipal
		seedOnce sync.Once
	)

	setupFixtures := func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources["datahub_local_user_login.steward"]
		if !ok {
			return fmt.Errorf("datahub_local_user_login.steward not found in state")
		}
		userURN := rs.Primary.Attributes["user_urn"]
		if userURN == "" {
			return fmt.Errorf("datahub_local_user_login.steward has no user_urn in state")
		}

		// The datasets need the domains to exist first, so they are seeded
		// here (post-apply) rather than in the test function.
		seedOnce.Do(func() {
			tg.EnsureDatasetEntityInDomain(t, dsFinance, financeURN)
			tg.EnsureDatasetEntityInDomain(t, dsBilling, billingURN)
			tg.EnsureDatasetEntityInDomain(t, dsMarketing, marketingURN)
			tg.EnsureDatasetEntity(t, dsOrphan)
			steward = tg.AsUser(t, userURN)
		})
		return nil
	}

	return []resource.TestStep{
		{
			Config: baseConfig,
			Check: resource.ComposeTestCheckFunc(
				setupFixtures,
				func(_ *terraform.State) error {
					// Negative control: no policy exists yet, so an in-domain
					// edit must be denied. This is what proves the minted
					// token is genuinely scoped -- a probe that passes here
					// invalidates every allow in step 2.
					res := AddTagAs(t, steward, tagURN, dsFinance)
					if !res.Denied() {
						return fmt.Errorf("steward addTag succeeded BEFORE any policy was created; " +
							"the steward client is not actually restricted, so the enforcement matrix cannot be trusted")
					}
					if !res.Unauthorized() {
						return fmt.Errorf("pre-policy addTag failed, but not with an authorization denial: %v", res.Errors)
					}
					return nil
				},
			),
		},
		{
			Config: policyConfig,
			Check: resource.ComposeTestCheckFunc(
				setupFixtures,
				func(_ *terraform.State) error {
					// Wait out the policy cache (120s refresh), then evaluate
					// the whole matrix against that same cache generation.
					AwaitPolicyEffect(t, 3*time.Minute+30*time.Second, func() GraphQLResult {
						return AddTagAs(t, steward, tagURN, dsFinance)
					})

					var failures []string
					record := func(format string, args ...any) {
						failures = append(failures, fmt.Sprintf(format, args...))
					}

					// Allow: sub-domain, via the DOMAIN criterion's descendant
					// cascade -- the policy names only the parent.
					if res := AddTagAs(t, steward, tagURN, dsBilling); res.Denied() {
						record("sub-domain addTag should be ALLOWED (cascade), got: %v", res.Errors)
					}
					// Deny: unrelated domain.
					if res := AddTagAs(t, steward, tagURN, dsMarketing); !res.Unauthorized() {
						record("other-domain addTag should be DENIED, got errors: %v", res.Errors)
					}
					// Deny: an asset with no domain matches nothing.
					if res := AddTagAs(t, steward, tagURN, dsOrphan); !res.Unauthorized() {
						record("no-domain addTag should be DENIED, got errors: %v", res.Errors)
					}
					// Deny: creating a tag is a platform privilege the policy
					// cannot and does not grant.
					if res := CreateTagAs(t, steward, base+"-denied", "Enforcement Denied"); !res.Unauthorized() {
						record("createTag should be DENIED (platform privilege), got errors: %v", res.Errors)
					}
					// Read access is catalog-wide (Reader role on Cloud; the
					// bootstrap all-users view policy already grants it on
					// OSS). The assertion is that our config leaves it intact.
					if !DatasetReadableAs(t, steward, dsMarketing) {
						record("steward cannot READ the out-of-domain dataset; read access should be catalog-wide")
					}

					if len(failures) > 0 {
						return fmt.Errorf("enforcement matrix failed:\n  - %s", strings.Join(failures, "\n  - "))
					}
					return nil
				},
			),
		},
	}
}
