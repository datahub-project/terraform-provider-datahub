// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

// Package importtarget provides a registry mapping each Terraform resource type
// in the datahub provider to its import-time metadata: how to enumerate existing
// entities in DataHub, how to convert a DataHub URN to the import ID the provider
// accepts, and whether the resource is OSS-compatible.
//
// Registrations live alongside their resource implementations as init() blocks
// in *_import_target.go files. The coverage test in coverage_test.go asserts
// that every resource registered with the provider either has a registry entry
// or an explicit exemption.
//
// The CLI (cmd/datahub-tf-import) iterates All() to drive bulk enumeration.
// The per-type enumeration data sources (e.g. data.datahub_ingestion_sources)
// also call their resource's Enumerate function via this registry.
package importtarget

import (
	"context"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// EnumerateFunc returns all URNs of a given entity type from DataHub. The
// slice may be empty if no entities exist; the caller distinguishes "none
// found" from errors via the error return value. A nil EnumerateFunc signals
// that auto-enumeration is not supported for this target (e.g. Cloud-only
// entities with no list API; users must supply IDs manually).
type EnumerateFunc func(ctx context.Context, c *datahub.Client) ([]string, error)

// Target holds the import-time metadata for one Terraform resource type.
type Target struct {
	// ResourceTypeName is the Terraform resource type, e.g. "datahub_ingestion_source".
	ResourceTypeName string

	// DataSourceTypeName is the companion enumeration data source, e.g.
	// "datahub_ingestion_sources" (plural). Empty if no data source is provided.
	DataSourceTypeName string

	// Enumerate lists all URNs of this entity type from DataHub. Nil when
	// auto-enumeration is not supported for this resource type.
	Enumerate EnumerateFunc

	// IDFromURN extracts the import ID that the provider's ImportState method
	// accepts from a full DataHub URN. For most resources this strips the
	// "urn:li:<type>:" prefix. May be nil if the import ID is the full URN.
	IDFromURN func(urn string) string

	// ConflictsWithGroups lists sets of attribute names that are mutually
	// exclusive in this resource's schema. The post-processor removes duplicates
	// when -generate-config-out emits both sides of a ConflictsWith pair.
	ConflictsWithGroups [][]string

	// OSSCompatible is false for Cloud-only resources and data sources.
	OSSCompatible bool
}

var registry []Target

// Register adds t to the registry. It is called from init() blocks in each
// *_import_target.go file alongside the corresponding resource implementation.
func Register(t Target) {
	registry = append(registry, t)
}

// All returns a copy of the full registry. Safe for concurrent reads after
// init() functions have run.
func All() []Target {
	out := make([]Target, len(registry))
	copy(out, registry)
	return out
}

// ByResourceType looks up a Target by its ResourceTypeName. Returns the zero
// value and false if no entry is found.
func ByResourceType(name string) (Target, bool) {
	for _, t := range registry {
		if t.ResourceTypeName == name {
			return t, true
		}
	}
	return Target{}, false
}
