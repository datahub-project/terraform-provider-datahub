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
