// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"fmt"
	"strings"
)

// DataHub rejects a structured property write with an HTTP 400 whose body is a
// ValidationExceptionCollection: an aspect dump with the actual reason buried in
// a msg= field. Several of those reasons have a fix the caller cannot guess from
// the text, and one is actively misleading -- the field-collision message
// explains itself in terms of '.' versus '_' normalisation, which describes a
// different situation entirely and sends the reader off renaming the property
// when renaming does not help.
//
// So the raw body is ALWAYS preserved and guidance is APPENDED, never
// substituted. Three reasons that shape everything below:
//
//   - The match is on server text we do not control. If DataHub rewords a
//     message our substring stops matching, and the user is left exactly where
//     they are today rather than worse off. A replacement would instead lose the
//     real error the moment upstream changed a word.
//   - If a substring matches something we did not mean, the reader can see that
//     for themselves against the original text sitting directly above.
//   - A write can fail several validators at once; the collection is a list. So
//     every hint that matches is appended, not just the first.
//
// Deliberately NOT covered: rejections whose own message already says what to do
// (qualified names cannot have spaces; allowedType must be an entity type urn).
// Adding a hint there is noise.

// structuredPropertyHint pairs a stable fragment of a DataHub validator message
// with advice that message does not carry.
type structuredPropertyHint struct {
	// match is the narrowest fragment that identifies the validator, chosen to
	// survive rewording around it. Compared case-insensitively.
	match string
	// hint is appended verbatim. It must stand alone: the reader has the raw
	// server text above it, not this file.
	hint string
}

// structuredPropertyHints enumerates the rejections worth explaining, drawn from
// PropertyDefinitionValidator in the DataHub server. Order is stable so a
// multi-failure response reads the same way every time.
var structuredPropertyHints = []structuredPropertyHint{
	{
		match: "collides with existing property",
		hint: "This property_id has been used before on this instance. Assigning a structured " +
			"property to an asset makes DataHub derive an Elasticsearch field for it, and deleting " +
			"the property does NOT release that field, so the name stays claimed. Set `version` to " +
			"a 14-digit timestamp (for example 20240610120000), higher than any value this property " +
			"has previously carried, to derive a fresh field name that does not collide. Disregard " +
			"the server's mention of '.' versus '_': that describes two different names colliding " +
			"with each other, not this. The behaviour is intended upstream -- see " +
			"https://github.com/datahub-project/datahub/issues/18974 -- and the alternative is an " +
			"operator-run index reindex.",
	},
	{
		match: "collides within the same batch",
		hint: "Two structured properties in the same apply normalise to one Elasticsearch field " +
			"name (DataHub replaces '.' with '_'), so `a.b` and `a_b` collide. Rename one of them.",
	},
	{
		match: "Invalid version specified",
		hint: "`version` must be exactly 14 digits, conventionally a yyyyMMddHHmmss timestamp such " +
			"as 20240610120000. Shorter forms like `v1` or `20240610` are rejected, despite being " +
			"suggested by DataHub's own field documentation.",
	},
	// Deliberately absent: "Invalid version `x` cannot contain the `.` character",
	// which lives in allowBreakingWithVersion. It cannot fire. versionFormatCheck
	// runs first in the same validation pass and rejects anything that is not
	// [0-9]{14}, which already excludes a dot, so the server can never reach its
	// own dots message. A hint here would be untestable and would imply a failure
	// mode users cannot hit.
	{
		match: "Cannot change the fully qualified name",
		hint: "`property_id` is the qualified name and cannot be edited in place. Terraform should " +
			"be planning a replacement here; if it is not, the state and the server disagree about " +
			"which property this resource manages.",
	},
	{
		match: "cardinality cannot be changed from MULTI to SINGLE",
		hint: "Narrowing cardinality is a breaking change. DataHub permits it only when `version` " +
			"increases in the same write, and the provider currently replaces the property rather " +
			"than updating it in place, so assigned values would not be preserved. Prefer a new " +
			"property_id.",
	},
	{
		match: "Value type cannot be changed",
		hint: "Changing `value_type` is a breaking change, permitted by DataHub only when `version` " +
			"increases in the same write. The provider currently replaces the property rather than " +
			"updating it in place, so assigned values would not be preserved. Prefer a new " +
			"property_id.",
	},
	{
		match: "Cannot restrict values that were previously allowed",
		hint: "Removing an entry from `allowed_values` is a breaking change, permitted by DataHub " +
			"only when `version` increases in the same write. Adding entries is always allowed.",
	},
	{
		match: "Cannot mutate a soft deleted",
		hint: "This property exists but is soft-deleted. Restore it in DataHub, or hard-delete it " +
			"before re-creating -- noting that a hard delete leaves its Elasticsearch field claimed.",
	},
	{
		match: "Unable to verify Elasticsearch field",
		hint: "DataHub could not read its own index mappings to check for a field-name collision, so " +
			"it refused the write rather than risk one. This is usually transient; retry.",
	},
}

// explainStructuredPropertyRejection returns body with any applicable guidance
// appended. The body is returned unchanged when nothing matches, which is the
// expected outcome for a rejection we have not catalogued and for any message
// DataHub rewords out from under us.
func explainStructuredPropertyRejection(body string) string {
	var hints []string
	for _, h := range structuredPropertyHints {
		if strings.Contains(strings.ToLower(body), strings.ToLower(h.match)) {
			hints = append(hints, "- "+h.hint)
		}
	}
	if len(hints) == 0 {
		return body
	}
	// Numbered only when there are several, so the single case stays terse.
	lead := "\n\nWhat this usually means:\n"
	if len(hints) > 1 {
		lead = fmt.Sprintf("\n\n%d validation failures were reported. What they usually mean:\n", len(hints))
	}
	return body + lead + strings.Join(hints, "\n")
}
