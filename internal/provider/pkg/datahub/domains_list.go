// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "context"

// ListDomainURNs returns the URNs of all DataHub domains, including nested
// domains at all levels of the hierarchy.
//
// Uses searchAcrossEntities with entity type DOMAIN. The search index is backed
// by OpenSearch and is eventually consistent. Entities created within the last
// few seconds may not appear. This function is intended for enumeration (extract
// tooling, inventory data sources), not for authoritative reads -- use
// GetDomainByURN for those.
func (c *Client) ListDomainURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "DOMAIN")
}
