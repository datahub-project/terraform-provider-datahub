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

// Term relationship types accepted by the addRelatedTerms/removeRelatedTerms
// mutations, verbatim from the GraphQL TermRelationshipType enum. The
// glossaryRelatedTerms aspect carries two further lists (values, relatedTerms)
// that have no GraphQL write path, so they are deliberately not exposed here.
const (
	TermRelationshipIsA  = "isA"
	TermRelationshipHasA = "hasA"
)

const glossaryTermURNPrefix = "urn:li:glossaryTerm:"

// TermRelationshipTypes returns the relationship types the client accepts
// (for validator and error messages).
func TermRelationshipTypes() []string {
	return []string{TermRelationshipIsA, TermRelationshipHasA}
}

func validateTermRelationshipInput(termURN, relationshipType, relatedTermURN string) error {
	if termURN == "" || relatedTermURN == "" {
		return errors.New("termURN and relatedTermURN are required")
	}
	if !strings.HasPrefix(termURN, glossaryTermURNPrefix) {
		return fmt.Errorf("termURN %q is not a glossaryTerm URN (expected prefix %q)", termURN, glossaryTermURNPrefix)
	}
	if !strings.HasPrefix(relatedTermURN, glossaryTermURNPrefix) {
		return fmt.Errorf("relatedTermURN %q is not a glossaryTerm URN (expected prefix %q)", relatedTermURN, glossaryTermURNPrefix)
	}
	if termURN == relatedTermURN {
		return errors.New("a glossary term cannot be related to itself")
	}
	if relationshipType != TermRelationshipIsA && relationshipType != TermRelationshipHasA {
		return fmt.Errorf("relationshipType %q is not valid; expected one of: %s", relationshipType, strings.Join(TermRelationshipTypes(), ", "))
	}
	return nil
}

// lockTermRelatedTerms serializes glossaryRelatedTerms-aspect writes to a
// single source term within this provider process, returning an unlock
// function (unlock := c.lockTermRelatedTerms(urn); defer unlock()).
//
// AddRelatedTermsResolver / RemoveRelatedTermsResolver perform a non-atomic
// read-modify-write of the source term's single glossaryRelatedTerms aspect
// server-side (getAspectFromEntity then persistAspect), the same shape as
// CAT-2568 for structuredProperties: concurrent writes to the SAME source term
// silently lose edges (last-writer-wins, no error). The provider models each
// relationship as its own resource, so Terraform fires these mutations in
// parallel at its default parallelism; serializing per source-term URN removes
// the race while leaving writes to different terms fully parallel.
func (c *Client) lockTermRelatedTerms(termURN string) func() {
	return c.relatedTermLocks.lock(termURN)
}

// relatedTermsMutation runs addRelatedTerms or removeRelatedTerms for a single
// (source term, relationship type, related term) edge.
func (c *Client) relatedTermsMutation(ctx context.Context, mutationName, termURN, relationshipType, relatedTermURN string) error {
	q := `
mutation ` + mutationName + `($input: RelatedTermsInput!) {
  ` + mutationName + `(input: $input)
}`
	body := map[string]any{
		"query": q,
		"variables": map[string]any{
			"input": map[string]any{
				"urn":              termURN,
				"termUrns":         []string{relatedTermURN},
				"relationshipType": relationshipType,
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

// AddRelatedTerm adds one typed relationship edge from a source glossary term
// to a related glossary term via the addRelatedTerms mutation. The edge is
// stored one-sided, on the source term's glossaryRelatedTerms aspect; the
// server writes no inverse edge on the related term. Idempotent: the server
// skips an edge that is already present.
//
// Both terms must already exist; the server rejects the write otherwise
// (unlike most DataHub aspects, related terms are validated referentially at
// write time).
func (c *Client) AddRelatedTerm(ctx context.Context, termURN, relationshipType, relatedTermURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if err := validateTermRelationshipInput(termURN, relationshipType, relatedTermURN); err != nil {
		return err
	}

	// Serialize writes to this source term's glossaryRelatedTerms aspect.
	unlock := c.lockTermRelatedTerms(termURN)
	defer unlock()

	return c.relatedTermsMutation(ctx, "addRelatedTerms", termURN, relationshipType, relatedTermURN)
}

// RemoveRelatedTerm removes one typed relationship edge from a source glossary
// term via the removeRelatedTerms mutation. Idempotent: if the source term or
// the edge is already gone it returns nil without calling the mutation, because
// the server errors when asked to remove from a term whose
// glossaryRelatedTerms aspect (or the type's list within it) does not exist.
func (c *Client) RemoveRelatedTerm(ctx context.Context, termURN, relationshipType, relatedTermURN string) error {
	if c == nil {
		return errors.New("client is nil")
	}
	if err := validateTermRelationshipInput(termURN, relationshipType, relatedTermURN); err != nil {
		return err
	}

	// Serialize writes to this source term's glossaryRelatedTerms aspect.
	unlock := c.lockTermRelatedTerms(termURN)
	defer unlock()

	rel, err := c.GetRelatedTerms(ctx, termURN)
	if err != nil {
		return err
	}
	if rel == nil || !rel.Has(relationshipType, relatedTermURN) {
		return nil // source term or edge already gone
	}

	return c.relatedTermsMutation(ctx, "removeRelatedTerms", termURN, relationshipType, relatedTermURN)
}

// RelatedTerms is the read-shape of a glossary term's glossaryRelatedTerms
// aspect, restricted to the two lists the GraphQL mutations can write.
type RelatedTerms struct {
	IsA  []string // terms this term inherits from ("Inherits" in the UI)
	HasA []string // terms this term contains ("Contains" in the UI)
}

// Has reports whether the given edge is present.
func (r *RelatedTerms) Has(relationshipType, relatedTermURN string) bool {
	if r == nil {
		return false
	}
	list := r.IsA
	if relationshipType == TermRelationshipHasA {
		list = r.HasA
	}
	for _, u := range list {
		if u == relatedTermURN {
			return true
		}
	}
	return false
}

// glossaryRelatedTermsEntity is the OpenAPI v3 response shape for
// GET /openapi/v3/entity/glossaryterm/{urn}, restricted to the
// glossaryRelatedTerms aspect.
type glossaryRelatedTermsEntity struct {
	URN          string `json:"urn"`
	RelatedTerms *struct {
		Value struct {
			IsRelatedTerms  []string `json:"isRelatedTerms"`
			HasRelatedTerms []string `json:"hasRelatedTerms"`
		} `json:"value"`
	} `json:"glossaryRelatedTerms,omitempty"`
}

// GetRelatedTerms reads a glossary term's glossaryRelatedTerms aspect via the
// strongly-consistent OpenAPI v3 entity endpoint. Returns nil (no error) when
// the term does not exist; a term with no relationships returns an empty
// RelatedTerms.
func (c *Client) GetRelatedTerms(ctx context.Context, termURN string) (*RelatedTerms, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	if !strings.HasPrefix(termURN, glossaryTermURNPrefix) {
		return nil, fmt.Errorf("termURN %q is not a glossaryTerm URN (expected prefix %q)", termURN, glossaryTermURNPrefix)
	}

	path := "/openapi/v3/entity/glossaryterm/" + termURN
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		respBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected HTTP %d reading glossaryRelatedTerms for %s: %s", res.StatusCode, termURN, respBody)
	}

	var entity glossaryRelatedTermsEntity
	if err := json.NewDecoder(res.Body).Decode(&entity); err != nil {
		return nil, fmt.Errorf("parsing glossaryRelatedTerms response: %w", err)
	}

	out := &RelatedTerms{}
	if entity.RelatedTerms != nil {
		out.IsA = entity.RelatedTerms.Value.IsRelatedTerms
		out.HasA = entity.RelatedTerms.Value.HasRelatedTerms
	}
	return out, nil
}

// RelatedTermExists reports whether the given relationship edge exists on the
// source term, reading via the strongly-consistent OpenAPI v3 entity endpoint.
// A missing source term is reported as "edge does not exist".
func (c *Client) RelatedTermExists(ctx context.Context, termURN, relationshipType, relatedTermURN string) (bool, error) {
	rel, err := c.GetRelatedTerms(ctx, termURN)
	if err != nil {
		return false, err
	}
	return rel.Has(relationshipType, relatedTermURN), nil
}
