// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"errors"
	"fmt"
	"strings"
)

// ErrFieldUnsupportedByInstance is returned when a write was refused because the
// instance's schema has no such input field. It is the version-skew counterpart
// to the ErrXCloudOnly sentinels: those mean "this whole feature is Cloud-only",
// this means "this instance is older than the attribute".
//
// DataHub Cloud runs a rolling fleet, so an instance that predates a recently
// added attribute is an ordinary situation rather than a misconfiguration. The
// provider deliberately carries no version detection (see the "Version and
// capability compatibility" section of docs/roadmap.md), so this sentinel is
// reached by reading what the server said, never by comparing version numbers.
var ErrFieldUnsupportedByInstance = errors.New("field not supported by this DataHub instance")

// UnsupportedFieldError describes a write refused for naming an input field the
// instance does not define. Attribute is the Terraform attribute name, which is
// what the practitioner has to edit; Field is the GraphQL input field, which is
// what the server named.
type UnsupportedFieldError struct {
	Attribute string
	Field     string
	Message   string
}

func (e *UnsupportedFieldError) Error() string {
	return fmt.Sprintf(
		"the DataHub instance does not appear to support the %s attribute; "+
			"attributes are sometimes added to this provider ahead of the DataHub releases that "+
			"support them, so check the provider CHANGELOG for when %s was introduced. "+
			"The server said: %s",
		e.Attribute, e.Attribute, e.Message,
	)
}

func (e *UnsupportedFieldError) Unwrap() error { return ErrFieldUnsupportedByInstance }

// unsupportedInputField reports whether a GraphQL error message names one of the
// optional input fields a write actually carried, and if so returns an error
// describing it in the practitioner's vocabulary.
//
// carried maps GraphQL input field name to Terraform attribute name, and is
// supplied by the caller rather than held in a central registry. The caller is
// the only place that knows which optional fields this particular write put on
// the wire, and that knowledge is what keeps the check safe: a message naming a
// field we did not send cannot be about a field we did not send.
//
// The match is on the field name alone, deliberately. GraphQL validates a
// variable's value against its input type before executing, and the rejection
// for an undefined input-object field names the field -- but graphql-java's
// exact phrasing varies by error class (WrongType, FieldUndefined, "is not a
// valid" ...), and guessing at it is how isOrganizationDisplayPreferencesCloudOnlyError
// came to miss the UnknownType shape and ship a raw error to users. Matching the
// name is the part that is safe to rely on.
//
// A false positive is mild: the server's own message is always preserved
// verbatim, so a genuine complaint about a field's *value* still reaches the
// user in full, merely alongside a suggestion that their instance may be older
// than the attribute.
func unsupportedInputField(msg string, carried map[string]string) error {
	for field, attribute := range carried {
		if strings.Contains(msg, field) {
			return &UnsupportedFieldError{Attribute: attribute, Field: field, Message: msg}
		}
	}
	return nil
}
