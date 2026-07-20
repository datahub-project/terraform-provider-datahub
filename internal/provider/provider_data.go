// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// providerData is the value the provider hands to every resource via
// resp.ResourceData: the API client plus provider-level default-labeling
// configuration. Data sources continue to receive the bare *datahub.Client.
type providerData struct {
	*datahub.Client
	defaults entityDefaults

	// verifiedTags memoizes tag-existence checks for defaults.tags URNs so
	// each tag is verified at most once per provider instance, however many
	// resources attach it.
	mu           sync.Mutex
	verifiedTags map[string]bool
}

// ensureTagsExist verifies that every tag URN exists in DataHub before it is
// attached anywhere. DataHub accepts globalTags writes referencing
// nonexistent tag URNs without complaint, which would silently produce
// dangling associations; failing fast with a clear message is preferable.
func (pd *providerData) ensureTagsExist(ctx context.Context, urns []string) error {
	for _, urn := range urns {
		pd.mu.Lock()
		ok := pd.verifiedTags[urn]
		pd.mu.Unlock()
		if ok {
			continue
		}
		tag, err := pd.GetTagByURN(ctx, urn)
		if err != nil {
			return fmt.Errorf("verifying default tag %s: %w", urn, err)
		}
		if tag == nil {
			return fmt.Errorf(
				"the tag %s referenced in the provider's defaults.tags does not exist in DataHub; "+
					"create it first (e.g. with a datahub_tag resource in a separate apply - provider "+
					"configuration cannot depend on resources created in the same apply)", urn)
		}
		pd.mu.Lock()
		if pd.verifiedTags == nil {
			pd.verifiedTags = map[string]bool{}
		}
		pd.verifiedTags[urn] = true
		pd.mu.Unlock()
	}
	return nil
}

// resourceProviderData extracts the *providerData from a resource Configure
// request. It returns nil without a diagnostic when ProviderData has not been
// set yet (the framework calls Configure before the provider is configured in
// some flows), and nil with an error diagnostic on a type mismatch.
func resourceProviderData(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *providerData {
	if req.ProviderData == nil {
		return nil
	}
	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *providerData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return nil
	}
	return pd
}
