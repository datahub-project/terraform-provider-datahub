// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	provider "github.com/datahub-project/terraform-provider-datahub/internal/provider"
)

// testAccProtoV6ProviderFactories is shared by all acceptance tests in this package.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"datahub": providerserver.NewProtocol6WithError(provider.New("test")()),
}
