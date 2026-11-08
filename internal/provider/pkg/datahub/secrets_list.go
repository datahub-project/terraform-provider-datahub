// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
)

// ListSecretURNs returns the URNs of all secrets in DataHub.
//
// It lists by the dataHubSecretValue aspect, as the listSecrets GraphQL
// resolver does, so a skeleton secret whose create never wrote a value is not
// returned. That aspect carries only the encrypted value, which this function
// never decodes: only the URN is kept.
//
// The listing bypasses DataHub's search result cache (see listEntityURNs) but
// is still backed by the search index, so a secret created within the last few
// seconds may not appear. This function is intended for enumeration (extract
// tooling, inventory data sources), not for authoritative reads -- use
// GetSecretByURN for those.
func (c *Client) ListSecretURNs(ctx context.Context) ([]string, error) {
	return c.listEntityURNs(ctx, "datahubsecret", "dataHubSecretValue", nil)
}
