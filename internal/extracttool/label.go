// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package extracttool

import (
	"regexp"
	"strconv"
	"strings"
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// LabelFromURN derives a valid Terraform identifier from a DataHub URN.
// It extracts the suffix after the last ':', lowercases it, replaces
// non-alphanumeric runs with underscores, and prepends "r_" when the
// result would start with a digit (invalid in Terraform identifiers).
func LabelFromURN(urn string) string {
	parts := strings.Split(urn, ":")
	suffix := parts[len(parts)-1]

	label := nonAlnum.ReplaceAllString(strings.ToLower(suffix), "_")
	label = strings.Trim(label, "_")

	if label == "" {
		label = "r"
	}
	if label[0] >= '0' && label[0] <= '9' {
		label = "r_" + label
	}
	if len(label) > 63 {
		label = label[:63]
	}
	return label
}

// uniqueLabels assigns a Terraform label to each URN in the slice.
// Duplicate base labels get a numeric suffix (_2, _3, ...).
func uniqueLabels(urns []string) []string {
	counts := make(map[string]int)
	bases := make([]string, len(urns))
	for i, urn := range urns {
		b := LabelFromURN(urn)
		bases[i] = b
		counts[b]++
	}

	seen := make(map[string]int)
	labels := make([]string, len(urns))
	for i, base := range bases {
		if counts[base] == 1 {
			labels[i] = base
		} else {
			seen[base]++
			n := seen[base]
			if n == 1 {
				labels[i] = base
			} else {
				labels[i] = base + "_" + strconv.Itoa(n)
			}
		}
	}
	return labels
}
