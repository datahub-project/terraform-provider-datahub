// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "context"

// ListGlossaryNodeURNs returns the URNs of all DataHub glossary nodes (Term
// Groups). Backed by searchAcrossEntities (OpenSearch, eventually consistent).
// Use GetGlossaryNodeByURN for authoritative per-entity reads.
func (c *Client) ListGlossaryNodeURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "GLOSSARY_NODE")
}

// ListGlossaryTermURNs returns the URNs of all DataHub glossary terms. Backed
// by searchAcrossEntities (OpenSearch, eventually consistent). Use
// GetGlossaryTermByURN for authoritative per-entity reads.
func (c *Client) ListGlossaryTermURNs(ctx context.Context) ([]string, error) {
	return listURNsByEntityType(ctx, c, "GLOSSARY_TERM")
}
