// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

// ossAndCloudBadge is a MarkdownDescription prefix for resources and data sources that
// work on both OSS DataHub and DataHub Cloud. Rendered output:
// DataHub ✅ | DataHub Cloud ✅.
const ossAndCloudBadge = "**DataHub ✅ | DataHub Cloud ✅**\n\n"

// cloudOnlyBadge is a MarkdownDescription prefix for resources and data sources that are
// available on DataHub Cloud only and will fail on OSS DataHub. Rendered output:
// DataHub ❌ | DataHub Cloud ✅.
const cloudOnlyBadge = "**DataHub ❌ | DataHub Cloud ✅**\n\n"

// newInProviderNote returns a MarkdownDescription prefix marking an attribute as
// recently added, naming the provider release that introduced it.
//
// The version named is deliberately *this provider's*, never DataHub's. DataHub
// has three numbering schemes in flight with no total order across them, so a
// backend version in a description is ambiguous and rots; the provider release
// that added an attribute is a fact fixed at authoring time that never changes,
// and it points the reader at the CHANGELOG entry describing what it needs.
//
// Use it for an attribute added to an already-shipped resource to cover a
// DataHub feature newer than the resource itself, where a practitioner on an
// older instance can read about the attribute on the registry page and have no
// way of knowing their server predates it. Delete the call once the fleet has
// converged -- it is a one-line diff and nothing depends on it.
func newInProviderNote(providerVersion string) string {
	return "**Added in provider " + providerVersion + ".** Requires a DataHub release that " +
		"supports it; see the " + providerVersion + " release notes. Configuring it against an " +
		"older instance fails with a clear error, and leaving it unset is always safe.\n\n"
}
