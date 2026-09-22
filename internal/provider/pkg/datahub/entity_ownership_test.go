// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"strings"
	"testing"
)

func TestOwnerEntityTypeFor(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		ownerURN string
		want     string
		wantErr  bool
	}{
		"corp user":                      {"urn:li:corpuser:alice@example.com", OwnerEntityTypeCorpUser, false},
		"corp group":                     {"urn:li:corpGroup:finance", OwnerEntityTypeCorpGroup, false},
		"service account is a corp user": {"urn:li:corpuser:service_etl", OwnerEntityTypeCorpUser, false},
		// The casing asymmetry is DataHub's, not a typo: the user URN type is
		// all-lowercase while the group one is camelCase. Getting it the wrong
		// way round is the most likely mistake here, so both directions are
		// asserted.
		"corpUser camelCase is not a user URN type": {"urn:li:corpUser:alice", "", true},
		"corpgroup lowercase is not a group URN type": {
			"urn:li:corpgroup:finance", "", true,
		},
		"dataset cannot own":         {"urn:li:dataset:(urn:li:dataPlatform:hive,a.b,PROD)", "", true},
		"data platform cannot own":   {"urn:li:dataPlatform:snowflake", "", true},
		"ownership type cannot own":  {"urn:li:ownershipType:steward", "", true},
		"bare user prefix":           {"urn:li:corpuser:", "", true},
		"bare group prefix":          {"urn:li:corpGroup:", "", true},
		"empty":                      {"", "", true},
		"not a urn":                  {"alice@example.com", "", true},
		"prefix appears mid-string":  {"prefix-urn:li:corpuser:alice", "", true},
		"corp user with empty local": {"urn:li:corpuser:x", OwnerEntityTypeCorpUser, false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := OwnerEntityTypeFor(tc.ownerURN)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("OwnerEntityTypeFor(%q) = %q, want an error", tc.ownerURN, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("OwnerEntityTypeFor(%q) returned %v", tc.ownerURN, err)
			}
			if got != tc.want {
				t.Errorf("OwnerEntityTypeFor(%q) = %q, want %q", tc.ownerURN, got, tc.want)
			}
		})
	}
}

// TestOwnershipTargetTypeAccepted pins both return values for every accepted
// target type. The path segment is what the OpenAPI v3 read URL is built from,
// so a wrong one produces a 404 that reads as "the entity does not exist" --
// which would silently remove the resource from state rather than failing.
func TestOwnershipTargetTypeAccepted(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		entityURN string
		wantPath  string
		wantType  string
	}{
		"domain":           {"urn:li:domain:finance", "domain", "domain"},
		"glossary term":    {"urn:li:glossaryTerm:revenue", "glossaryterm", "glossaryTerm"},
		"glossary node":    {"urn:li:glossaryNode:finance", "glossarynode", "glossaryNode"},
		"data product":     {"urn:li:dataProduct:orders", "dataproduct", "dataProduct"},
		"corp group":       {"urn:li:corpGroup:finance", "corpgroup", "corpGroup"},
		"tag":              {"urn:li:tag:PII", "tag", "tag"},
		"form":             {"urn:li:form:compliance", "form", "form"},
		"ingestion source": {"urn:li:dataHubIngestionSource:nightly", "datahubingestionsource", "dataHubIngestionSource"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotPath, gotType, err := OwnershipTargetType(tc.entityURN)
			if err != nil {
				t.Fatalf("OwnershipTargetType(%q) returned %v", tc.entityURN, err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path segment = %q, want %q", gotPath, tc.wantPath)
			}
			if gotType != tc.wantType {
				t.Errorf("entity type = %q, want %q", gotType, tc.wantType)
			}
		})
	}

	// Every accepted type must be named in the diagnostic list, or a rejection
	// message would tell the practitioner to use a type it does not mention.
	listed := strings.Join(SupportedOwnershipEntityTypes(), ",")
	for _, tc := range tests {
		if !strings.Contains(listed, tc.wantType) {
			t.Errorf("SupportedOwnershipEntityTypes() omits %q: %s", tc.wantType, listed)
		}
	}
}

// TestOwnershipTargetTypeRejected asserts the three rejection reasons stay
// distinguishable. They call for three different responses -- enrich the asset
// in the UI, accept that the aspect does not exist, or fix the typo -- so a
// single generic message would be a regression even though the URN is refused
// either way.
func TestOwnershipTargetTypeRejected(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		entityURN   string
		wantPhrases []string
	}{
		"dataset is a data asset": {
			"urn:li:dataset:(urn:li:dataPlatform:hive,a.b,PROD)",
			[]string{"ingested data asset", "out of scope for this provider"},
		},
		"chart is a data asset": {
			"urn:li:chart:(looker,baz)",
			[]string{"ingested data asset"},
		},
		"container is a data asset": {
			"urn:li:container:abc123",
			[]string{"ingested data asset"},
		},
		// corpuser and dataContract are accepted structured-property assignment
		// targets, so a reader familiar with that resource would expect them here
		// too. They carry no ownership aspect, which is a different failure with
		// no workaround.
		"corpuser has no ownership aspect": {
			"urn:li:corpuser:alice@example.com",
			[]string{"corpuser", "does not declare the DataHub `ownership` aspect at all", "Unknown aspect ownership for entity corpuser"},
		},
		"data contract has no ownership aspect": {
			"urn:li:dataContract:abc",
			[]string{"dataContract", "does not declare the DataHub `ownership` aspect at all"},
		},
		"policy has no ownership aspect": {
			"urn:li:dataHubPolicy:abc",
			[]string{"dataHubPolicy", "does not declare the DataHub `ownership` aspect at all"},
		},
		"structured property has no ownership aspect": {
			"urn:li:structuredProperty:io.acme.tier",
			[]string{"structuredProperty", "does not declare the DataHub `ownership` aspect at all"},
		},
		"unrecognised entity type": {
			"urn:li:mysteryEntity:whatever",
			[]string{"not a supported owner assignment target"},
		},
		"empty urn": {
			"",
			[]string{"not a supported owner assignment target"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := OwnershipTargetType(tc.entityURN)
			if err == nil {
				t.Fatalf("OwnershipTargetType(%q) accepted a URN it must reject", tc.entityURN)
			}
			for _, phrase := range tc.wantPhrases {
				if !strings.Contains(err.Error(), phrase) {
					t.Errorf("error for %q does not mention %q:\n%v", tc.entityURN, phrase, err)
				}
			}
		})
	}
}

// TestEffectiveOwnershipTypeURN covers the read-path mapping that lets an owner
// edge stored with only the legacy Owner.type enum be recognised as the pair a
// configuration declares. The mapping reproduces the server's own
// OwnerUtils.mapOwnershipTypeToEntity: __system__ plus the lowercased enum name.
func TestEffectiveOwnershipTypeURN(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		typeURN    string
		legacyType string
		want       string
	}{
		"typeUrn wins when present": {
			"urn:li:ownershipType:io.acme.steward", "NONE", "urn:li:ownershipType:io.acme.steward",
		},
		"typeUrn wins even over a meaningful legacy type": {
			"urn:li:ownershipType:io.acme.steward", "TECHNICAL_OWNER", "urn:li:ownershipType:io.acme.steward",
		},
		"technical owner maps to its system entity": {
			"", "TECHNICAL_OWNER", "urn:li:ownershipType:__system__technical_owner",
		},
		"business owner maps to its system entity": {
			"", "BUSINESS_OWNER", "urn:li:ownershipType:__system__business_owner",
		},
		"data steward maps to its system entity": {
			"", "DATA_STEWARD", "urn:li:ownershipType:__system__data_steward",
		},
		"none maps to its system entity": {
			"", "NONE", "urn:li:ownershipType:__system__none",
		},
		"a deprecated enum value still maps": {
			"", "DATAOWNER", "urn:li:ownershipType:__system__dataowner",
		},
		// Owner.type is non-optional in the PDL so this should not occur, but a
		// directly written aspect could omit it; returning "" is what makes the
		// edge fail to match any declared pair rather than matching the wrong one.
		"neither field set": {"", "", ""},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := EffectiveOwnershipTypeURN(tc.typeURN, tc.legacyType); got != tc.want {
				t.Errorf("EffectiveOwnershipTypeURN(%q, %q) = %q, want %q", tc.typeURN, tc.legacyType, got, tc.want)
			}
		})
	}
}

// TestBatchAddOwnersRejectsUntypedPair asserts the client refuses to send an
// OwnerInput without an ownership type rather than letting the server
// materialise __system__none behind the caller's back, which would leave the
// pair unaddressable by the configuration that created it.
func TestBatchAddOwnersRejectsUntypedPair(t *testing.T) {
	t.Parallel()

	c, err := NewClient("http://example.invalid", "token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = c.BatchAddOwners(t.Context(), "urn:li:domain:finance", []OwnerEdge{
		{OwnerURN: "urn:li:corpuser:alice", OwnershipTypeURN: ""},
	})
	if err == nil {
		t.Fatal("BatchAddOwners accepted an owner with no ownership type URN")
	}
	if !strings.Contains(err.Error(), "ownership type URN is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRemoveOwnerRequiresOwnershipType is the same guard on the removal side,
// and the more important one: RemoveOwnerInput.ownershipTypeUrn is optional in
// the schema, and omitting it removes the owner under EVERY ownership type it
// holds. No caller may do that by accident.
func TestRemoveOwnerRequiresOwnershipType(t *testing.T) {
	t.Parallel()

	c, err := NewClient("http://example.invalid", "token")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = c.RemoveOwner(t.Context(), "urn:li:domain:finance", "urn:li:corpuser:alice", "")
	if err == nil {
		t.Fatal("RemoveOwner accepted an empty ownership type URN, which would remove every ownership type for that owner")
	}
	if !strings.Contains(err.Error(), "removes that owner under every ownership type") {
		t.Errorf("unexpected error: %v", err)
	}
}
