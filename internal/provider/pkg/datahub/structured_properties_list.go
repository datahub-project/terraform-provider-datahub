// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "context"

// ListStructuredPropertyURNs returns the URNs of all DataHub structured
// properties visible to the authenticated principal.
//
// Uses searchAcrossEntities with entity type STRUCTURED_PROPERTY. The search
// index is backed by OpenSearch and is eventually consistent. Properties created
// within the last few seconds may not appear. This function is intended for
// enumeration (extract tooling, inventory data sources), not for authoritative
// reads -- use GetStructuredPropertyByURN for those.
func (c *Client) ListStructuredPropertyURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "STRUCTURED_PROPERTY")
}
