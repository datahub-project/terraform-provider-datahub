// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"net"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
)

// isLoopbackEndpoint reports whether host names the local machine. Plaintext
// HTTP to loopback is the documented DataHub Quickstart setup
// (http://localhost:8080), so warning about it would be noise on the one path
// the project's own Makefile and tests use.
func isLoopbackEndpoint(hostname string) bool {
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	// Covers 127.0.0.0/8 and ::1 without enumerating them.
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// warnIfPlaintextEndpoint warns when the resolved GMS endpoint uses plaintext
// HTTP to a non-loopback host.
//
// The GMS token is sent on every request as an Authorization bearer header, so
// over http it is a long-lived credential travelling in cleartext, readable by
// anything on the path. Nothing else in the provider says so.
//
// A warning rather than an error: http to a non-loopback host is legitimate for
// an instance reached over a private network or an SSH tunnel, and refusing it
// would break those setups for a risk the operator may already have accounted
// for. Making it visible is the useful part.
//
// This lives in Configure rather than as a schema validator on gms_url because a
// schema validator sees only the configuration attribute, never
// DATAHUB_GMS_URL - which is how CI and every runnable example supply the value.
// A validator here would check one of the two credential vectors and imply it
// covered both.
func warnIfPlaintextEndpoint(resp *provider.ConfigureResponse, host string) {
	if host == "" {
		return
	}

	parsed, err := url.Parse(host)
	if err != nil {
		// An unparseable host is NewClient's problem to report; this function
		// only ever adds a warning, so staying quiet is right.
		return
	}

	// A host supplied without a scheme is upgraded to https by NewClient, so
	// only an explicit http:// warrants the warning.
	if !strings.EqualFold(parsed.Scheme, "http") {
		return
	}
	if isLoopbackEndpoint(parsed.Hostname()) {
		return
	}

	resp.Diagnostics.AddAttributeWarning(
		path.Root("gms_url"),
		"DataHub endpoint uses plaintext HTTP",
		"The GMS endpoint "+parsed.Redacted()+" uses http, so the GMS token is sent as a "+
			"bearer credential in cleartext on every request and is readable by anything "+
			"on the network path. Use https unless the endpoint is reached over a private "+
			"network or a tunnel that already provides transport security.",
	)
}
