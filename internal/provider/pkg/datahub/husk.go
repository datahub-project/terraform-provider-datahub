// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// isEntityHusk reports whether the entity at urn is a resurrection husk: it
// exists, has no info/properties aspect, and carries nothing beyond its key
// aspect and an empty structuredProperties aspect. This is exactly the shape
// DataHub's PropertyDefinitionDeleteSideEffect leaves behind when its cleanup
// patch lands on a hard-deleted entity (CAT-2583): the patch recreates the key
// aspect, so the URN blocks re-creation with "already exists" while rendering
// nowhere in the UI or search.
//
// keyAspectName and infoAspectName are the entity type's key aspect and its
// content-bearing aspect (e.g. "domainKey" and "domainProperties"). Any other
// shape -- the info aspect present, values still assigned, or any unexpected
// aspect -- disqualifies the entity, so a real pre-existing entity is never
// mistaken for debris. Callers must treat both a false result and an error as
// "not a husk" and surface their original failure untouched.
func (c *Client) isEntityHusk(ctx context.Context, entityPath, keyAspectName, infoAspectName, urn string) (bool, error) {
	path := fmt.Sprintf("/openapi/v3/entity/%s/%s", entityPath, urn)
	req, err := c.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	res, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode >= http.StatusBadRequest {
		return false, fmt.Errorf("unexpected HTTP %d from DataHub entity API", res.StatusCode)
	}

	var aspects map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&aspects); err != nil {
		return false, fmt.Errorf("parsing entity response: %w", err)
	}

	if _, hasInfo := aspects[infoAspectName]; hasInfo {
		return false, nil
	}
	for name, raw := range aspects {
		switch name {
		case "urn", keyAspectName:
			// expected on any entity
		case "structuredProperties":
			var sp struct {
				Value struct {
					Properties []json.RawMessage `json:"properties"`
				} `json:"value"`
			}
			if err := json.Unmarshal(raw, &sp); err != nil || len(sp.Value.Properties) > 0 {
				return false, nil
			}
		default:
			return false, nil
		}
	}
	return true, nil
}
