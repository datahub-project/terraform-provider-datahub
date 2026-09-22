// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// The handlers here are written from DataHub's GraphQL schema and from
// OwnerUtils / OwnerServiceUtils, not from what the provider's client happens
// to send. A mock derived from client behaviour agrees with any client bug --
// #116 shipped a green mock that hid three live defects -- so every rule below
// cites the server code it mirrors.
//
// The five rules that matter, all of them load-bearing for
// datahub_entity_ownership:
//
//  1. batchAddOwners is a per-(owner, ownership type) UPSERT.
//     OwnerServiceUtils.addOwnerToAspect calls removeExistingOwnerIfExists with
//     the same ownership type before appending, so re-adding a pair is a no-op
//     and other ownership types for the same owner survive.
//  2. A null ownershipTypeUrn on OwnerInput is materialised server-side, not
//     stored as null: OwnerUtils maps the legacy type enum through
//     mapOwnershipTypeToEntity to urn:li:ownershipType:__system__<lowercased>.
//     So typeUrn is always populated after a GraphQL add.
//  3. removeOwner with a null ownershipTypeUrn removes EVERY edge for that
//     owner. OwnerServiceUtils.isOwnerEqual returns true for any owner-URN
//     match the moment the requested type is null. The mock reproduces this
//     exactly, which is what lets a test prove the provider never relies on it.
//  4. OwnerUtils.validateOwners resolves every owner URN and every ownership
//     type URN against EntityService and refuses the whole batch when one is
//     missing. This is the reason the provider writes owners through GraphQL
//     rather than through an OpenAPI v3 aspect write.
//  5. removeOwner does NOT validate owners (RemoveOwnerResolver calls only
//     validateRemoveInput), so removing an absent pair succeeds.
//
// Error strings are the server's own, verbatim from OwnerUtils.validateOwner.

// mockOwnerEdge is one element of a stored ownership aspect's owners array.
// Type is the legacy Owner.type enum, which the PDL marks @deprecated but not
// optional, so it is always present on read.
type mockOwnerEdge struct {
	Owner   string
	Type    string
	TypeURN string
}

// systemOwnershipTypePrefix marks the ownership type entities DataHub
// bootstraps for the legacy enum values. They are real entities server-side, so
// they resolve in validation without appearing in the mock's ownershipTypes map
// (which holds only types a test created).
const systemOwnershipTypePrefix = "urn:li:ownershipType:__system__"

// ownerPrincipalExists mirrors the EntityService resolution
// OwnerUtils.validateOwners performs on each owner URN. Caller must hold s.mu.
func (s *mockServer) ownerPrincipalExists(urn string) bool {
	if id, ok := strings.CutPrefix(urn, "urn:li:corpuser:"); ok {
		_, found := s.users[id]
		return found
	}
	if id, ok := strings.CutPrefix(urn, "urn:li:corpGroup:"); ok {
		_, found := s.groups[id]
		return found
	}
	return false
}

// ownershipTypeExists mirrors the same resolution for an ownership type URN.
// Caller must hold s.mu.
func (s *mockServer) ownershipTypeExists(urn string) bool {
	if strings.HasPrefix(urn, systemOwnershipTypePrefix) {
		return true
	}
	id, ok := strings.CutPrefix(urn, "urn:li:ownershipType:")
	if !ok {
		return false
	}
	_, found := s.ownershipTypes[id]
	return found
}

// handleBatchAddOwners handles the batchAddOwners GraphQL mutation.
func (s *mockServer) handleBatchAddOwners(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	rawOwners, _ := input["owners"].([]any)
	rawResources, _ := input["resources"].([]any)

	type parsedOwner struct {
		ownerURN        string
		ownerEntityType string
		legacyType      string
		typeURN         string
	}
	owners := make([]parsedOwner, 0, len(rawOwners))
	for _, ro := range rawOwners {
		o, _ := ro.(map[string]any)
		p := parsedOwner{}
		p.ownerURN, _ = o["ownerUrn"].(string)
		p.ownerEntityType, _ = o["ownerEntityType"].(string)
		p.legacyType, _ = o["type"].(string)
		p.typeURN, _ = o["ownershipTypeUrn"].(string)
		owners = append(owners, p)
	}

	resourceURNs := make([]string, 0, len(rawResources))
	for _, rr := range rawResources {
		res, _ := rr.(map[string]any)
		if sub, ok := res["subResource"].(string); ok && sub != "" {
			// BatchAddOwnersResolver rejects this outright.
			writeOwnershipMockError(w, "Malformed input provided: owners cannot be applied to subresources.")
			return
		}
		urn, _ := res["resourceUrn"].(string)
		if urn != "" {
			resourceURNs = append(resourceURNs, urn)
		}
	}

	s.mu.Lock()
	// OwnerUtils.validateOwner, in the server's own order.
	for _, o := range owners {
		switch {
		case o.ownerEntityType == "CORP_GROUP" && !strings.HasPrefix(o.ownerURN, "urn:li:corpGroup:"):
			s.mu.Unlock()
			writeOwnershipMockError(w, fmt.Sprintf(
				"Failed to change ownership for resource(s). Expected a corp group urn, found %s", o.ownerURN))
			return
		case o.ownerEntityType == "CORP_USER" && !strings.HasPrefix(o.ownerURN, "urn:li:corpuser:"):
			s.mu.Unlock()
			writeOwnershipMockError(w, fmt.Sprintf(
				"Failed to change ownership for resource(s). Expected a corp user urn, found %s.", o.ownerURN))
			return
		}
		if !s.ownerPrincipalExists(o.ownerURN) {
			s.mu.Unlock()
			writeOwnershipMockError(w, fmt.Sprintf(
				"Failed to change ownership for resource(s). Owner with urn %s does not exist.", o.ownerURN))
			return
		}
		if o.typeURN != "" && !s.ownershipTypeExists(o.typeURN) {
			s.mu.Unlock()
			writeOwnershipMockError(w, fmt.Sprintf(
				"Failed to change ownership for resource(s). Custom Ownership type with urn %s does not exist.", o.typeURN))
			return
		}
		if o.legacyType == "" && o.typeURN == "" {
			s.mu.Unlock()
			writeOwnershipMockError(w,
				"Failed to change ownership for resource(s). Expected either type or ownershipTypeUrn to be specified.")
			return
		}
	}

	for _, resourceURN := range resourceURNs {
		for _, o := range owners {
			legacy := o.legacyType
			if legacy == "" {
				legacy = "NONE"
			}
			typeURN := o.typeURN
			if typeURN == "" {
				typeURN = systemOwnershipTypePrefix + strings.ToLower(legacy)
			}
			s.addOwnerLocked(resourceURN, mockOwnerEdge{Owner: o.ownerURN, Type: legacy, TypeURN: typeURN})
		}
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"batchAddOwners": true},
	})
}

// addOwnerLocked appends an edge after removing any existing edge with the same
// owner AND the same ownership type, which is what makes the add an upsert
// per pair rather than per owner. Caller must hold s.mu.
func (s *mockServer) addOwnerLocked(entityURN string, edge mockOwnerEdge) {
	existing := s.entityOwners[entityURN]
	kept := make([]mockOwnerEdge, 0, len(existing)+1)
	for _, e := range existing {
		if ownerEdgeMatches(e, edge.Owner, edge.TypeURN) {
			continue
		}
		kept = append(kept, e)
	}
	s.entityOwners[entityURN] = append(kept, edge)
}

// ownerEdgeMatches mirrors OwnerServiceUtils.isOwnerEqual, including the
// branch that makes a null ownership type match every edge for the owner.
func ownerEdgeMatches(e mockOwnerEdge, ownerURN, ownershipTypeURN string) bool {
	if e.Owner != ownerURN {
		return false
	}
	if e.TypeURN != "" && ownershipTypeURN != "" {
		return e.TypeURN == ownershipTypeURN
	}
	if ownershipTypeURN == "" {
		return true
	}
	// Fall back to deriving the system ownership type entity from the legacy
	// enum, as the server does.
	return systemOwnershipTypePrefix+strings.ToLower(e.Type) == ownershipTypeURN
}

// handleRemoveOwner handles the removeOwner GraphQL mutation. It performs no
// owner or ownership-type validation, matching RemoveOwnerResolver.
func (s *mockServer) handleRemoveOwner(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	ownerURN, _ := input["ownerUrn"].(string)
	resourceURN, _ := input["resourceUrn"].(string)
	ownershipTypeURN, _ := input["ownershipTypeUrn"].(string)

	s.mu.Lock()
	existing := s.entityOwners[resourceURN]
	kept := make([]mockOwnerEdge, 0, len(existing))
	for _, e := range existing {
		if ownerEdgeMatches(e, ownerURN, ownershipTypeURN) {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(s.entityOwners, resourceURN)
	} else {
		s.entityOwners[resourceURN] = kept
	}
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{"removeOwner": true},
	})
}

// writeOwnershipMockError writes a GraphQL-style errors response (HTTP 200 with
// an errors array), the shape the client inspects.
func writeOwnershipMockError(w http.ResponseWriter, msg string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{"message": msg}},
	})
}

// ownershipAspect returns the OpenAPI v3 ownership aspect for an entity, or nil
// when no owner has ever been set -- the real endpoint omits the key entirely
// in that case rather than returning an empty array, and the provider's Read
// has to tolerate exactly that.
//
// ownerTypes is included because the real aspect carries it (populated by a
// server-side mutation hook), and a provider that round-tripped the aspect body
// would send it back. Rendering it here is what would catch that.
//
// Takes its own lock; call it from item handlers AFTER they have released s.mu.
func (s *mockServer) ownershipAspect(entityURN string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	edges := s.entityOwners[entityURN]
	if len(edges) == 0 {
		return nil
	}
	owners := make([]map[string]any, 0, len(edges))
	ownerTypes := map[string][]string{}
	for _, e := range edges {
		owner := map[string]any{
			"owner":  e.Owner,
			"type":   e.Type,
			"source": map[string]any{"type": "MANUAL"},
		}
		if e.TypeURN != "" {
			owner["typeUrn"] = e.TypeURN
			ownerTypes[e.TypeURN] = append(ownerTypes[e.TypeURN], e.Owner)
		}
		owners = append(owners, owner)
	}
	return map[string]any{"value": map[string]any{
		"owners":       owners,
		"ownerTypes":   ownerTypes,
		"lastModified": map[string]any{"time": 1758499200000, "actor": "urn:li:corpuser:datahub"},
	}}
}

// seedOwnerRequest is the body of POST /test-control/seed-owner.
type seedOwnerRequest struct {
	EntityURN        string `json:"entityUrn"`
	OwnerURN         string `json:"ownerUrn"`
	OwnershipTypeURN string `json:"ownershipTypeUrn"`
	// LegacyType sets Owner.type. Defaults to NONE, which is what a GraphQL
	// write that supplied only ownershipTypeUrn produces.
	LegacyType string `json:"legacyType"`
	// OmitTypeURN stores the edge with Owner.typeUrn absent and only the legacy
	// enum set, as an aspect written directly (or before typeUrn existed) would
	// look. Used to prove the provider's legacy-type fallback on the read path.
	OmitTypeURN bool `json:"omitTypeUrn"`
}

// handleSeedOwner injects an owner edge straight into the stored aspect,
// bypassing the mutation entirely -- the mock equivalent of somebody assigning
// an owner in the DataHub UI. It is what makes the merge contract testable:
// there is no other way to have an owner present that Terraform never declared.
//
//	POST /test-control/seed-owner {"entityUrn": ..., "ownerUrn": ..., "ownershipTypeUrn": ...}
func (s *mockServer) handleSeedOwner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req seedOwnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.EntityURN == "" || req.OwnerURN == "" {
		http.Error(w, "entityUrn and ownerUrn are required", http.StatusBadRequest)
		return
	}
	legacy := req.LegacyType
	if legacy == "" {
		legacy = "NONE"
	}
	edge := mockOwnerEdge{Owner: req.OwnerURN, Type: legacy, TypeURN: req.OwnershipTypeURN}
	if req.OmitTypeURN {
		edge.TypeURN = ""
	}

	s.mu.Lock()
	s.addOwnerLocked(req.EntityURN, edge)
	s.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// handleDropOwners clears every owner from an entity's stored aspect, leaving
// the ownership key absent from the entity read -- the state of an entity that
// has never had an owner, and the one the provider's Read has to tolerate.
//
//	POST /test-control/drop-owners {"entityUrn": ...}
func (s *mockServer) handleDropOwners(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		EntityURN string `json:"entityUrn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EntityURN == "" {
		http.Error(w, "entityUrn is required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	delete(s.entityOwners, req.EntityURN)
	s.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}
