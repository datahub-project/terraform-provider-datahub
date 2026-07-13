// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// providerData is the value the provider hands to every resource via
// resp.ResourceData: the API client plus provider-level default-labeling
// configuration. Data sources continue to receive the bare *datahub.Client.
type providerData struct {
	*datahub.Client
	defaults entityDefaults
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
