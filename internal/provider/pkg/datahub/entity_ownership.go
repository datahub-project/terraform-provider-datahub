// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OwnershipTypeURNPrefix is the URN prefix of every ownership type entity,
// custom or built-in.
const OwnershipTypeURNPrefix = "urn:li:ownershipType:"

// systemOwnershipTypeURNPrefix is the prefix DataHub gives the ownership type
// entities it bootstraps for the legacy Owner.type enum values, e.g.
// TECHNICAL_OWNER -> urn:li:ownershipType:__system__technical_owner. The
// server derives it in OwnerUtils.mapOwnershipTypeToEntity, and this provider
// reproduces that mapping on the read path (see OwnerEdgesFromAspect).
const systemOwnershipTypeURNPrefix = OwnershipTypeURNPrefix + "__system__"

// corpUserURNPrefix and corpGroupURNPrefix are the only two owner URN kinds
// DataHub's OwnerEntityType enum can express. Note the asymmetric casing: the
// user URN type is all-lowercase "corpuser" while the group one is "corpGroup".
const (
	corpUserURNPrefix  = "urn:li:corpuser:"
	corpGroupURNPrefix = "urn:li:corpGroup:"
)

// OwnerEntityType values, verbatim from DataHub's OwnerEntityType GraphQL enum,
// which has exactly these two members.
const (
	OwnerEntityTypeCorpUser  = "CORP_USER"
	OwnerEntityTypeCorpGroup = "CORP_GROUP"
)

// OwnerEdge is one (owner, ownership type) pair on one entity: a single element
// of the entity's ownership aspect owners array.
//
// The pair is the identity. Neither half is unique on its own: several owners
// commonly share one ownership type, and one owner commonly holds several
// ownership types. Both are ordinary in DataHub and both must round-trip.
type OwnerEdge struct {
	OwnerURN         string
	OwnershipTypeURN string
}

// ownershipTargets is the set of entity types the provider accepts as owner
// assignment targets. Membership requires two independent things:
//
//  1. the entity type declares the `ownership` aspect in DataHub's
//     entity-registry.yml -- otherwise the write is rejected at ingestion with
//     "Unknown aspect ownership for entity <name>";
//  2. the entity type is platform configuration this provider already manages,
//     rather than an ingested data asset (see ownershipAssetTypes).
//
// Verified against metadata-models/src/main/resources/entity-registry.yml.
// Deliberately NOT a copy of the structured-property assignment allowlist:
// that one includes corpuser and dataContract, and neither carries `ownership`.
//
// pathSegment is the lowercase OpenAPI v3 entity path; entityType is the short
// registry name used in diagnostics.
var ownershipTargets = []struct {
	urnPrefix   string
	pathSegment string
	entityType  string
}{
	{"urn:li:domain:", "domain", "domain"},
	{"urn:li:glossaryTerm:", "glossaryterm", "glossaryTerm"},
	{"urn:li:glossaryNode:", "glossarynode", "glossaryNode"},
	{"urn:li:dataProduct:", "dataproduct", "dataProduct"},
	{corpGroupURNPrefix, "corpgroup", "corpGroup"},
	{"urn:li:tag:", "tag", "tag"},
	{"urn:li:form:", "form", "form"},
	{"urn:li:dataHubIngestionSource:", "datahubingestionsource", "dataHubIngestionSource"},
}

// ownershipNoAspectTypes are config entity types a practitioner might
// reasonably try -- three of them are valid structured-property assignment
// targets -- that do not declare the `ownership` aspect at all. They earn their
// own diagnostic because the reason is the opposite of the deny-list one: not
// "out of scope", but "there is nothing there to write to".
var ownershipNoAspectTypes = map[string]string{
	corpUserURNPrefix:            "corpuser",
	"urn:li:dataContract:":       "dataContract",
	"urn:li:dataHubPolicy:":      "dataHubPolicy",
	"urn:li:structuredProperty:": "structuredProperty",
}

// ownershipAssetTypes are entity types that do carry `ownership` but whose
// metadata is owned by ingestion or by business users editing the catalog. The
// provider's charter excludes per-asset enrichment, because every apply would
// overwrite what those users curated by hand.
var ownershipAssetTypes = []string{
	"urn:li:dataset:",
	"urn:li:chart:",
	"urn:li:dashboard:",
	"urn:li:dataJob:",
	"urn:li:dataFlow:",
	"urn:li:container:",
	"urn:li:notebook:",
	"urn:li:mlModel:",
	"urn:li:mlModelGroup:",
	"urn:li:mlFeature:",
	"urn:li:mlFeatureTable:",
	"urn:li:mlPrimaryKey:",
	"urn:li:schemaField:",
	"urn:li:dataPlatformInstance:",
	"urn:li:application:",
	"urn:li:businessAttribute:",
}

// SupportedOwnershipEntityTypes returns the short entity-type names the provider
// accepts as owner assignment targets (for validator and diagnostic messages).
func SupportedOwnershipEntityTypes() []string {
	out := make([]string, len(ownershipTargets))
	for i, t := range ownershipTargets {
		out[i] = t.entityType
	}
	return out
}

// OwnershipTargetType resolves a target entity URN to its OpenAPI v3 path
// segment and its short registry entity-type name.
//
// It errors for every URN the provider does not accept, with three distinct
// messages, because the three reasons call for three different fixes: an
// unmodelled aspect cannot be worked around at all, a data asset should be
// enriched through the UI, and an unrecognised type is probably a typo.
func OwnershipTargetType(entityURN string) (pathSegment, entityType string, err error) {
	for _, t := range ownershipTargets {
		if strings.HasPrefix(entityURN, t.urnPrefix) {
			return t.pathSegment, t.entityType, nil
		}
	}
	for prefix, name := range ownershipNoAspectTypes {
		if strings.HasPrefix(entityURN, prefix) {
			return "", "", fmt.Errorf(
				"entity URN %q is a %s, which does not declare the DataHub `ownership` aspect at all "+
					"(verified against entity-registry.yml), so there is nothing for owners to be written to; "+
					"DataHub rejects the write with \"Unknown aspect ownership for entity %s\". "+
					"Supported target types: %s",
				entityURN, name, name, strings.Join(SupportedOwnershipEntityTypes(), ", "))
		}
	}
	for _, prefix := range ownershipAssetTypes {
		if strings.HasPrefix(entityURN, prefix) {
			return "", "", fmt.Errorf(
				"entity URN %q is an ingested data asset; assigning owners to data assets "+
					"(datasets, charts, dashboards, ...) is out of scope for this provider, because that "+
					"metadata belongs to ingestion and to business users editing the catalog and every "+
					"apply would overwrite their edits. Supported target types: %s",
				entityURN, strings.Join(SupportedOwnershipEntityTypes(), ", "))
		}
	}
	return "", "", fmt.Errorf(
		"entity URN %q is not a supported owner assignment target; supported types: %s",
		entityURN, strings.Join(SupportedOwnershipEntityTypes(), ", "))
}

// OwnerEntityTypeFor derives the OwnerEntityType enum value DataHub's
// OwnerInput requires from the owner's own URN, so the practitioner never has
// to restate in configuration something the URN already says.
//
// DataHub's OwnerEntityType enum has exactly two members, and OwnerUtils
// cross-checks the declared member against the URN's entity type, so any other
// owner URN kind is rejected here rather than sent to be refused there.
func OwnerEntityTypeFor(ownerURN string) (string, error) {
	switch {
	case strings.HasPrefix(ownerURN, corpUserURNPrefix) && ownerURN != corpUserURNPrefix:
		return OwnerEntityTypeCorpUser, nil
	case strings.HasPrefix(ownerURN, corpGroupURNPrefix) && ownerURN != corpGroupURNPrefix:
		return OwnerEntityTypeCorpGroup, nil
	}
	return "", fmt.Errorf(
		"owner URN %q is neither a corp user (%s...) nor a corp group (%s...); DataHub's "+
			"OwnerEntityType has exactly those two members, so nothing else can own an entity",
		ownerURN, corpUserURNPrefix, corpGroupURNPrefix)
}

// BatchAddOwners adds every supplied (owner, ownership type) pair to one
// entity in a single batchAddOwners mutation.
//
// Why GraphQL and not an OpenAPI v3 aspect write, which would be one HTTP call
// with no enum derivation: OwnerUtils.validateOwners resolves every owner URN
// and every ownership-type URN against EntityService and refuses the whole
// batch when one is missing ("Owner with urn %s does not exist." / "Custom
// Ownership type with urn %s does not exist."). An aspect write bypasses that
// and silently persists a dangling owner reference, which nothing later
// surfaces. So the mutation is what buys owner-existence validation for free.
// Do not "optimise" this into an aspect write.
//
// The add is a per-(owner, type) upsert server-side -- OwnerServiceUtils
// removes any existing edge with the same owner AND the same ownership type
// before appending -- so this is idempotent, and owners already on the entity
// under other ownership types, or added out of band, are left intact.
func (c *Client) BatchAddOwners(ctx context.Context, entityURN string, owners []OwnerEdge) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if entityURN == "" {
		return errors.New("entityURN is required")
	}
	if len(owners) == 0 {
		return nil
	}
	if _, _, err := OwnershipTargetType(entityURN); err != nil {
		return err
	}

	ownerInputs := make([]map[string]any, 0, len(owners))
	for _, o := range owners {
		ownerEntityType, err := OwnerEntityTypeFor(o.OwnerURN)
		if err != nil {
			return err
		}
		if o.OwnershipTypeURN == "" {
			// A null ownershipTypeUrn is accepted by the schema and materialised
			// server-side into urn:li:ownershipType:__system__none, which would
			// make the pair unaddressable by the caller. Never send one.
			return fmt.Errorf("ownership type URN is required for owner %q", o.OwnerURN)
		}
		ownerInputs = append(ownerInputs, map[string]any{
			"ownerUrn":         o.OwnerURN,
			"ownerEntityType":  ownerEntityType,
			"ownershipTypeUrn": o.OwnershipTypeURN,
		})
	}

	const q = `
mutation batchAddOwners($input: BatchAddOwnersInput!) {
  batchAddOwners(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"owners":    ownerInputs,
				"resources": []map[string]any{{"resourceUrn": entityURN}},
			},
		},
	}

	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// RemoveOwner removes exactly one (owner, ownership type) pair from one entity.
//
// ownershipTypeURN is mandatory even though RemoveOwnerInput declares it
// optional, and that is the whole point of this method's shape.
// OwnerServiceUtils.isOwnerEqual short-circuits to true for every edge whose
// owner URN matches as soon as the requested ownership type is null, so
// omitting it removes the owner under EVERY ownership type it holds -- silently,
// and including ones this resource never declared. Pinning the pair is what
// keeps the merge contract honest.
//
// Idempotent: RemoveOwnerResolver validates only that the target entity exists,
// so removing a pair that is already absent (or whose owner was deleted out of
// band) succeeds.
func (c *Client) RemoveOwner(ctx context.Context, entityURN, ownerURN, ownershipTypeURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if entityURN == "" || ownerURN == "" {
		return errors.New("entityURN and ownerURN are required")
	}
	if ownershipTypeURN == "" {
		return fmt.Errorf(
			"ownership type URN is required when removing owner %q from %q: a removeOwner call "+
				"without one removes that owner under every ownership type it holds",
			ownerURN, entityURN)
	}

	const q = `
mutation removeOwner($input: RemoveOwnerInput!) {
  removeOwner(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"ownerUrn":         ownerURN,
				"ownershipTypeUrn": ownershipTypeURN,
				"resourceUrn":      entityURN,
			},
		},
	}

	var gqlResp genericGraphQLErrors
	if err := c.doGraphQL(ctx, body, &gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("DataHub API error: %s", gqlResp.Errors[0].Message)
	}
	return nil
}

// ownershipAspectEntity is the OpenAPI v3 read shape of the `ownership` aspect
// on any target entity. The aspect key is absent entirely when no owner has
// ever been set -- not present with an empty array -- which is why Ownership is
// a pointer.
type ownershipAspectEntity struct {
	URN       string `json:"urn"`
	Ownership *struct {
		Value struct {
			Owners []ownershipAspectOwner `json:"owners"`
		} `json:"value"`
	} `json:"ownership,omitempty"`
}

// ownershipAspectOwner is one element of ownership.value.owners. Both type
// fields are read: Owner.type is @deprecated but non-optional in the PDL so it
// is always present, while Owner.typeUrn is optional in the model and in
// practice always populated by any GraphQL write.
type ownershipAspectOwner struct {
	Owner   string `json:"owner"`
	Type    string `json:"type"`
	TypeURN string `json:"typeUrn"`
}

// EffectiveOwnershipTypeURN resolves one owners-array element to the ownership
// type URN that identifies its pair.
//
// typeUrn wins when present. When it is absent the legacy `type` enum is mapped
// to the bootstrapped system ownership type entity the same way the server does
// in OwnerUtils.mapOwnershipTypeToEntity: __system__ plus the lowercased enum
// name. Reproducing that mapping is what lets a pair written before typeUrn
// existed, or written directly to the aspect, still be recognised as the pair
// the configuration declares -- without it such an edge reads as absent and the
// resource shows a diff it can never settle.
func EffectiveOwnershipTypeURN(typeURN, legacyType string) string {
	if typeURN != "" {
		return typeURN
	}
	if legacyType == "" {
		return ""
	}
	return systemOwnershipTypeURNPrefix + strings.ToLower(legacyType)
}

// GetEntityOwners reads every (owner, ownership type) pair on one entity
// through the strongly-consistent OpenAPI v3 entity endpoint (MySQL-backed).
//
// GraphQL list and search queries are OpenSearch-backed and would report a pair
// written moments ago as absent, which is exactly the shape of the spurious
// "plan to delete" that this provider hit once already.
//
// found is false when the entity does not exist. An existing entity whose
// ownership aspect is absent returns an empty slice and found=true: no owners
// is a legitimate state, not a missing entity.
func (c *Client) GetEntityOwners(ctx context.Context, entityURN string) ([]OwnerEdge, bool, error) {
	if c == nil {
		return nil, false, errors.New("client is nil")
	}
	pathSegment, _, err := OwnershipTargetType(entityURN)
	if err != nil {
		return nil, false, err
	}

	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", pathSegment, entityURN)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, false, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, false, fmt.Errorf("DataHub rejected the request (HTTP %d): the calling principal needs the Edit Owners privilege on %s", res.StatusCode, entityURN)
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, false, fmt.Errorf("unexpected HTTP %d reading ownership for %s: %s", res.StatusCode, entityURN, respBody)
	}

	var entity ownershipAspectEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, false, fmt.Errorf("parsing ownership response: %w", err)
	}
	if entity.Ownership == nil {
		return []OwnerEdge{}, true, nil
	}

	edges := make([]OwnerEdge, 0, len(entity.Ownership.Value.Owners))
	for _, o := range entity.Ownership.Value.Owners {
		if o.Owner == "" {
			continue
		}
		edges = append(edges, OwnerEdge{
			OwnerURN:         o.Owner,
			OwnershipTypeURN: EffectiveOwnershipTypeURN(o.TypeURN, o.Type),
		})
	}
	return edges, true, nil
}
