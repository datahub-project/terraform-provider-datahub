// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"context"
	"encoding/json"
	"fmt"
)

// ListIngestionSourceURNs returns the URNs of all user-managed ingestion sources
// in DataHub.
//
// DataHub Cloud provisions internal "system" ingestion sources (datahub-gc,
// datahub-usage-reporting, semantic-anchor, user-entity-resolution, ...) that
// run platform maintenance jobs. They are owned by urn:li:corpuser:__datahub_system
// and must never be brought under Terraform management -- the datahub_ingestion_source
// resource cannot meaningfully own them and an apply would fight the platform.
// They are identified by dataHubIngestionSourceInfo.source.type == "SYSTEM" and
// are filtered out here, so both the extract CLI and the
// datahub_ingestion_sources data source only surface user-managed sources.
//
// The listing bypasses DataHub's search result cache (see listEntityURNs) but
// is still backed by the search index, so a source created within the last few
// seconds may not appear. This function is intended for enumeration (extract
// tooling, inventory data sources), not for authoritative reads -- use
// GetIngestionSourceByID for those.
func (c *Client) ListIngestionSourceURNs(ctx context.Context) ([]string, error) {
	return c.listEntityURNs(ctx, "datahubingestionsource", "dataHubIngestionSourceInfo",
		func(raw json.RawMessage) (bool, error) {
			var info struct {
				Value struct {
					Source *struct {
						Type string `json:"type"`
					} `json:"source"`
				} `json:"value"`
			}
			if err := json.Unmarshal(raw, &info); err != nil {
				return false, fmt.Errorf("decoding source type: %w", err)
			}
			return info.Value.Source == nil || info.Value.Source.Type != "SYSTEM", nil
		})
}
