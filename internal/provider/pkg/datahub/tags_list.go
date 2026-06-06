// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "context"

// ListTagURNs returns the URNs of all DataHub tags visible to the authenticated
// principal.
//
// Uses searchAcrossEntities with entity type TAG. The search index is backed by
// OpenSearch and is eventually consistent. Tags created within the last few
// seconds may not appear. This function is intended for enumeration (extract
// tooling, inventory data sources), not for authoritative reads -- use
// GetTagByURN for those.
func (c *Client) ListTagURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "TAG")
}
