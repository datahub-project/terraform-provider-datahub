// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"errors"
	"strings"
	"testing"
)

func TestUnsupportedInputField(t *testing.T) {
	carried := map[string]string{"primaryColor": "primary_color"}

	tests := []struct {
		name    string
		msg     string
		carried map[string]string
		want    bool
	}{
		{
			// graphql-java's phrasing varies by error class, which is why the
			// match is on the field name rather than on the wording. Each of
			// these is a shape a server could plausibly return.
			name:    "WrongType phrasing",
			msg:     "Validation error of type WrongType: argument 'input.primaryColor' with value 'StringValue{value='#EC0016'}' is not a valid 'String' @ 'updateOrganizationDisplayPreferences'",
			carried: carried,
			want:    true,
		},
		{
			name:    "FieldUndefined phrasing",
			msg:     "Validation error of type FieldUndefined: Field 'primaryColor' is not defined",
			carried: carried,
			want:    true,
		},
		{
			name:    "unknown field phrasing",
			msg:     "Unknown field 'primaryColor' on input object 'UpdateOrganizationDisplayPreferencesInput'",
			carried: carried,
			want:    true,
		},
		{
			// An unrelated server complaint must stay an ordinary API error, or
			// every failure would be blamed on the instance being old.
			name:    "unrelated error is not claimed",
			msg:     "Unauthorized to update organization display preferences",
			carried: carried,
			want:    false,
		},
		{
			// The guard that makes matching on the name alone safe: a write that
			// did not carry the field cannot be refused for carrying it, so the
			// candidate set is empty and nothing can match.
			name:    "field not sent is never matched",
			msg:     "Validation error: Field 'primaryColor' is not defined",
			carried: map[string]string{},
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := unsupportedInputField(tc.msg, tc.carried)
			if (err != nil) != tc.want {
				t.Fatalf("unsupportedInputField() = %v, want match=%v", err, tc.want)
			}
			if !tc.want {
				return
			}
			if !errors.Is(err, ErrFieldUnsupportedByInstance) {
				t.Errorf("error does not unwrap to ErrFieldUnsupportedByInstance: %v", err)
			}
			// The practitioner has to edit the Terraform attribute, so that is
			// what the message must name -- not the GraphQL field.
			if !strings.Contains(err.Error(), "primary_color") {
				t.Errorf("message does not name the Terraform attribute: %q", err.Error())
			}
			// The server's own words are always preserved, so a false positive
			// is informative rather than misleading.
			if !strings.Contains(err.Error(), tc.msg) {
				t.Errorf("message drops the server's text: %q", err.Error())
			}
		})
	}
}
