// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "context"

// ListDataProductURNs returns the URNs of all DataHub data products visible
// to the authenticated principal.
//
// Uses searchAcrossEntities with entity type DATA_PRODUCT. The search index
// is backed by OpenSearch and is eventually consistent. Data products created
// within the last few seconds may not appear. This function is intended for
// enumeration (extract tooling, inventory data sources), not for authoritative
// reads -- use GetDataProductByURN for those.
func (c *Client) ListDataProductURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "DATA_PRODUCT")
}
