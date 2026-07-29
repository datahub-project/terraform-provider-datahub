// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// Server-bug diagnostic, tracked upstream as CAT-2583. Deleting a structured
// property triggers a server-side side effect that scrolls the eventually
// consistent search index for entities carrying the property and JSON-PATCHes
// each hit. A patch against an entity that was concurrently hard-deleted
// resurrects it as an empty husk: the key aspect is written back, but the
// content aspect that makes it a real entity (domainProperties, tagProperties,
// ...) is gone. The husk is invisible in the UI, yet it still blocks
// re-creation of the same URN with "already exists".
//
// The typed client getters cannot see this distinction: they flatten the
// OpenAPI v3 entity response and report "present" whenever the key aspect
// alone is there, so a husk and a genuinely undeleted entity produce the same
// CheckDestroy failure. That ambiguity left a real nightly flake in
// TestAcc_SPDefaults_AssignmentOverlap undiagnosed for weeks, because the
// message gave no way to tell a resurrection from a broken Delete without
// re-running against a live instance and inspecting aspects by hand.
//
// stillExistsAfterDestroyError closes that gap: it re-reads the raw entity
// document, reports which aspects are present, and names the shape. Husk or
// not, the entity is still a failure - a husk blocks re-creation just as
// effectively - so this only sharpens the message, it never tolerates
// anything. Remove it once CAT-2583 is fixed upstream and resurrection is no
// longer a possible explanation.

// aspectShape records which aspects the OpenAPI v3 entity endpoint reports for
// a single URN. found is false when the endpoint has no document for the URN
// (HTTP 404, or a body carrying no aspects at all).
type aspectShape struct {
	found   bool
	aspects []string
}

// contentlessAspects are aspects whose presence says nothing about whether the
// entity still has a definition. The key aspect (matched separately, by its
// "Key" suffix) is written by any write to any aspect of the URN;
// structuredProperties is exactly what the CAT-2583 side effect patches; and
// status only records soft-delete state.
var contentlessAspects = map[string]bool{
	"status":               true,
	"structuredProperties": true,
}

// isHuskShape reports whether the aspect set is a husk: at least one aspect is
// present, but none of them carries entity content.
func isHuskShape(aspects []string) bool {
	if len(aspects) == 0 {
		return false
	}
	for _, a := range aspects {
		if strings.HasSuffix(a, "Key") || contentlessAspects[a] {
			continue
		}
		return false
	}
	return true
}

// entityPathFromURN derives the OpenAPI v3 entity path segment from a URN:
// "urn:li:domain:x" gives "domain", "urn:li:dataHubIngestionSource:x" gives
// "datahubingestionsource". The endpoint path segment is always the lowercased
// URN entity type.
func entityPathFromURN(urn string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(urn), ":", 4)
	if len(parts) < 4 || parts[0] != "urn" || parts[1] != "li" || parts[2] == "" {
		return "", fmt.Errorf("cannot derive entity type from URN %q", urn)
	}
	return strings.ToLower(parts[2]), nil
}

// probeAspectShape reads GET /openapi/v3/entity/{type}/{urn} and returns the
// names of the aspects present in the response. It reads the raw document
// rather than a typed getter precisely because the typed getters discard the
// information this diagnostic needs.
func probeAspectShape(ctx context.Context, client *datahub.Client, urn string) (aspectShape, error) {
	if client == nil {
		return aspectShape{}, errors.New("client is nil")
	}
	entityPath, err := entityPathFromURN(urn)
	if err != nil {
		return aspectShape{}, err
	}

	req, err := client.NewRequest(ctx, http.MethodGet,
		"/openapi/v3/entity/"+entityPath+"/"+url.PathEscape(urn), nil)
	if err != nil {
		return aspectShape{}, fmt.Errorf("building entity request for %s: %w", urn, err)
	}
	res, err := client.Do(req)
	if err != nil {
		return aspectShape{}, fmt.Errorf("reading entity document for %s: %w", urn, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode == http.StatusNotFound {
		return aspectShape{found: false}, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return aspectShape{}, fmt.Errorf("unexpected HTTP %d from GET /openapi/v3/entity/%s/%s: %s",
			res.StatusCode, entityPath, urn, strings.TrimSpace(string(body)))
	}

	var doc map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		return aspectShape{}, fmt.Errorf("parsing entity document for %s: %w", urn, err)
	}

	aspects := make([]string, 0, len(doc))
	for k := range doc {
		// "urn" is the document envelope, not an aspect.
		if k == "urn" {
			continue
		}
		aspects = append(aspects, k)
	}
	sort.Strings(aspects)
	return aspectShape{found: len(aspects) > 0, aspects: aspects}, nil
}

// describeStillExists renders the CheckDestroy failure message for an entity
// the server still reports after destroy, naming the shape so the failure is
// self-diagnosing. Pure so it can be unit tested against both shapes.
func describeStillExists(resourceType, urn string, shape aspectShape, probeErr error) string {
	lead := fmt.Sprintf("%s %q still exists after destroy", resourceType, urn)

	switch {
	case probeErr != nil:
		return lead + fmt.Sprintf("; the aspect-shape probe failed (%v), so whether this is a "+
			"CAT-2583 resurrected husk or a genuine delete failure is undetermined - "+
			"read the entity document by hand to classify it", probeErr)
	case !shape.found:
		return lead + "; the aspect-shape probe found no aspects for this URN, contradicting the read " +
			"that reported it present (an index or replication artifact, or a delete that landed between " +
			"the two reads), so CAT-2583 resurrected husk versus genuine delete failure is undetermined"
	case isHuskShape(shape.aspects):
		return lead + fmt.Sprintf("; aspects present: [%s] - key and contentless aspects only, no content "+
			"aspect, so this is a CAT-2583 resurrected husk: deleting a structured property patched this "+
			"entity back into existence after it was hard-deleted. The husk is invisible in the DataHub UI "+
			"but still blocks re-creation of the same URN, so it is a failure, not noise",
			strings.Join(shape.aspects, ", "))
	default:
		return lead + fmt.Sprintf("; aspects present: [%s] - the entity still carries its content aspects, "+
			"so this is a genuine delete failure and not a CAT-2583 resurrection",
			strings.Join(shape.aspects, ", "))
	}
}

// stillExistsAfterDestroyError builds the CheckDestroy failure for an entity
// that a typed getter still reports as present after destroy. The returned
// error always fails the check; the aspect probe only classifies the failure.
func stillExistsAfterDestroyError(ctx context.Context, client *datahub.Client, resourceType, urn string) error {
	shape, probeErr := probeAspectShape(ctx, client, urn)
	return errors.New(describeStillExists(resourceType, urn, shape, probeErr))
}
