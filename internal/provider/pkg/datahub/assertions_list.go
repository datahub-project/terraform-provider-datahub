// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "context"

// ListAssertionURNs returns the URNs of all DataHub assertions visible to the
// authenticated principal.
//
// Uses searchAcrossEntities with entity type ASSERTION. The search index is
// backed by OpenSearch and is eventually consistent. Assertions created within
// the last few seconds may not appear. This function is intended for
// enumeration (extract tooling, inventory data sources), not for authoritative
// reads -- use GetAssertionByURN for those.
func (c *Client) ListAssertionURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "ASSERTION")
}
