// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"errors"
	"fmt"
	"testing"
)

// TestIsOrganizationDisplayPreferencesCloudOnlyError covers the OSS detection
// heuristic. The real signal is a GraphQL validation error for a mutation field
// that does not exist in the OSS schema.
func TestIsOrganizationDisplayPreferencesCloudOnlyError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{
			name: "OSS missing mutation",
			msg:  "Validation error of type FieldUndefined: Field 'updateOrganizationDisplayPreferences' in type 'Mutation' is undefined",
			want: true,
		},
		{
			name: "undefined field on a different type is not our signal",
			msg:  "Validation error of type FieldUndefined: Field 'somethingElse' in type 'Query' is undefined",
			want: false,
		},
		{
			name: "authorization failure is a real error, not a Cloud-only signal",
			msg:  "Unauthorized to perform this action. Please contact your DataHub administrator.",
			want: false,
		},
		{
			name: "empty",
			msg:  "",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isOrganizationDisplayPreferencesCloudOnlyError(tc.msg); got != tc.want {
				t.Errorf("isOrganizationDisplayPreferencesCloudOnlyError(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestOrganizationDisplayPreferencesCloudOnlyErrorSurvivesWrapping guards the
// resource's diagnostic path: the resource wraps the client error before
// inspecting it with errors.Is, so a sentinel that did not unwrap cleanly would
// silently downgrade the "requires DataHub Cloud" diagnostic to a raw GraphQL
// error. Only the nightly OSS job exercises that path end to end, so assert the
// unwrapping here.
func TestOrganizationDisplayPreferencesCloudOnlyErrorSurvivesWrapping(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("writing organization display preferences: %w", ErrOrganizationDisplayPreferencesCloudOnly)
	if !errors.Is(wrapped, ErrOrganizationDisplayPreferencesCloudOnly) {
		t.Fatal("wrapped Cloud-only sentinel no longer matches errors.Is; the resource would emit a raw API error instead of the Cloud-only diagnostic")
	}

	other := fmt.Errorf("writing organization display preferences: %w", errors.New("boom"))
	if errors.Is(other, ErrOrganizationDisplayPreferencesCloudOnly) {
		t.Fatal("unrelated error matched the Cloud-only sentinel")
	}
}
