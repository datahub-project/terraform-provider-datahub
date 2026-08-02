// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// The acceptance framework can assert an error diagnostic but has no way to
// assert a warning, so the severity split that this whole guard turns on --
// roles refuse, editable warns -- is only checkable here. So is the wording,
// and the wording is the deliverable: a user who hits this cannot repair the
// damage through the DataHub UI, so the message has to carry the URN, the
// roles at stake, the upstream issue, and a way out.

func rolePolicy(roles ...string) *datahub.Policy {
	return &datahub.Policy{
		URN:        "urn:li:dataHubPolicy:admin-platform-policy",
		ID:         "admin-platform-policy",
		Name:       "Admins - Platform Policy",
		Type:       "PLATFORM",
		State:      "ACTIVE",
		Privileges: []string{"MANAGE_POLICIES"},
		ActorRoles: roles,
		Editable:   true,
	}
}

func severities(diags diag.Diagnostics) (errs, warns int) {
	for _, d := range diags {
		if d.Severity() == diag.SeverityError {
			errs++
		}
		if d.Severity() == diag.SeverityWarning {
			warns++
		}
	}
	return errs, warns
}

func TestPolicyWriteHazards(t *testing.T) {
	t.Parallel()

	t.Run("no_live_policy_is_not_a_hazard", func(t *testing.T) {
		t.Parallel()
		// A genuine create: nothing exists at the URN, so there is nothing to
		// lose. Every ordinary apply takes this path, and a guard that fired
		// here would break the resource outright.
		if diags := policyWriteHazards(nil); diags.HasError() || len(diags) != 0 {
			t.Fatalf("policyWriteHazards(nil) = %v, want no diagnostics", diags)
		}
	})

	t.Run("role_free_editable_policy_is_not_a_hazard", func(t *testing.T) {
		t.Parallel()
		p := rolePolicy()
		if diags := policyWriteHazards(p); len(diags) != 0 {
			t.Fatalf("policyWriteHazards(role-free) = %v, want no diagnostics", diags)
		}
	})

	t.Run("roles_produce_an_error", func(t *testing.T) {
		t.Parallel()
		diags := policyWriteHazards(rolePolicy("urn:li:dataHubRole:Admin"))
		errs, warns := severities(diags)
		if errs != 1 || warns != 0 {
			t.Fatalf("got %d errors and %d warnings, want exactly 1 error: %v", errs, warns, diags)
		}

		detail := diags[0].Detail()
		for _, want := range []string{
			"urn:li:dataHubPolicy:admin-platform-policy", // which policy
			"urn:li:dataHubRole:Admin",                   // which grant is at stake
			"granting its privileges to nobody",          // the consequence
			"OSS-1216",                                   // where the defect lives
			"terraform state rm",                         // the way out
			"/openapi/v3/entity/datahubpolicy",           // the deliberate override
		} {
			if !strings.Contains(detail, want) {
				t.Errorf("refusal detail is missing %q:\n%s", want, detail)
			}
		}
		// Pinning the provider version is not a mitigation and must not be
		// implied: the loss happens server-side.
		if strings.Contains(detail, "upgrade the provider") {
			t.Errorf("refusal detail implies a provider upgrade helps:\n%s", detail)
		}
	})

	t.Run("every_role_is_named", func(t *testing.T) {
		t.Parallel()
		diags := policyWriteHazards(rolePolicy(
			"urn:li:dataHubRole:Admin", "urn:li:dataHubRole:Editor"))
		detail := diags[0].Detail()
		if !strings.Contains(detail, "urn:li:dataHubRole:Admin") ||
			!strings.Contains(detail, "urn:li:dataHubRole:Editor") {
			t.Errorf("refusal detail does not name both roles:\n%s", detail)
		}
	})

	t.Run("other_actor_bindings_change_the_consequence", func(t *testing.T) {
		t.Parallel()
		// Roles alongside named users: the policy would still grant something
		// afterwards, so claiming it would grant nothing to nobody would be a
		// lie, and an operator who spotted the lie would stop trusting the
		// rest of the message.
		p := rolePolicy("urn:li:dataHubRole:Admin")
		p.Actors.Users = []string{"urn:li:corpuser:datahub"}

		detail := policyWriteHazards(p)[0].Detail()
		if strings.Contains(detail, "granting its privileges to nobody") {
			t.Errorf("refusal claims the policy would grant nothing despite a user binding:\n%s", detail)
		}
		if !strings.Contains(detail, "other actor bindings would survive") {
			t.Errorf("refusal does not say the surviving bindings survive:\n%s", detail)
		}
	})

	t.Run("all_users_counts_as_a_binding", func(t *testing.T) {
		t.Parallel()
		p := rolePolicy("urn:li:dataHubRole:Admin")
		p.Actors.AllUsers = true
		if strings.Contains(policyWriteHazards(p)[0].Detail(), "granting its privileges to nobody") {
			t.Error("allUsers=true policy described as granting to nobody")
		}
	})

	t.Run("resource_owners_counts_as_a_binding", func(t *testing.T) {
		t.Parallel()
		// The one bootstrap policy that binds through resource ownership alone
		// would otherwise be described as empty.
		p := rolePolicy("urn:li:dataHubRole:Admin")
		p.Actors.ResourceOwners = true
		if strings.Contains(policyWriteHazards(p)[0].Detail(), "granting its privileges to nobody") {
			t.Error("resourceOwners=true policy described as granting to nobody")
		}
	})

	t.Run("non_editable_warns_but_does_not_block", func(t *testing.T) {
		t.Parallel()
		p := rolePolicy()
		p.Editable = false

		diags := policyWriteHazards(p)
		errs, warns := severities(diags)
		if errs != 0 || warns != 1 {
			t.Fatalf("got %d errors and %d warnings, want exactly 1 warning: %v", errs, warns, diags)
		}
		detail := diags[0].Detail()
		for _, want := range []string{
			"editable = false",
			"resets the flag to true",
			"every deployment", // the bootstrap re-ingest, which is why this fights
		} {
			if !strings.Contains(detail, want) {
				t.Errorf("non-editable warning is missing %q:\n%s", want, detail)
			}
		}
	})

	t.Run("roles_and_non_editable_report_both", func(t *testing.T) {
		t.Parallel()
		// Eight of DataHub's nine role-bearing bootstrap policies are both.
		p := rolePolicy("urn:li:dataHubRole:Admin")
		p.Editable = false

		errs, warns := severities(policyWriteHazards(p))
		if errs != 1 || warns != 1 {
			t.Fatalf("got %d errors and %d warnings, want 1 of each", errs, warns)
		}
	})

	t.Run("unnamed_policy_falls_back_to_the_urn", func(t *testing.T) {
		t.Parallel()
		p := rolePolicy("urn:li:dataHubRole:Admin")
		p.Name = ""
		detail := policyWriteHazards(p)[0].Detail()
		if !strings.Contains(detail, "Policy urn:li:dataHubPolicy:admin-platform-policy grants") {
			t.Errorf("unnamed policy is not identified by URN:\n%s", detail)
		}
	})
}

func TestPolicyImportHazards(t *testing.T) {
	t.Parallel()

	t.Run("roles_warn_rather_than_fail_the_import", func(t *testing.T) {
		t.Parallel()
		// Import reads; it destroys nothing. Refusing it would leave a user
		// who bulk-imported unable even to see what they had adopted.
		diags := policyImportHazards(rolePolicy("urn:li:dataHubRole:Editor"))
		errs, warns := severities(diags)
		if errs != 0 || warns != 1 {
			t.Fatalf("got %d errors and %d warnings, want exactly 1 warning: %v", errs, warns, diags)
		}

		detail := diags[0].Detail()
		for _, want := range []string{
			"urn:li:dataHubRole:Editor",
			"the provider will refuse it",
			"OSS-1216",
			"datahub_policies",   // where the accidental adoption comes from
			"datahub-tf-extract", // and the other place
			"terraform state rm",
		} {
			if !strings.Contains(detail, want) {
				t.Errorf("import warning is missing %q:\n%s", want, detail)
			}
		}
	})

	t.Run("role_free_import_is_silent", func(t *testing.T) {
		t.Parallel()
		if diags := policyImportHazards(rolePolicy()); len(diags) != 0 {
			t.Fatalf("policyImportHazards(role-free) = %v, want no diagnostics", diags)
		}
	})

	t.Run("nil_is_silent", func(t *testing.T) {
		t.Parallel()
		if diags := policyImportHazards(nil); len(diags) != 0 {
			t.Fatalf("policyImportHazards(nil) = %v, want no diagnostics", diags)
		}
	})
}

// TestPolicyImportStateDiagnostics proves the import path is actually wired to
// the guard, which the acceptance suite cannot: TestStep has ExpectError but no
// equivalent for a warning, so an ImportState that quietly stopped calling
// policyImportHazards would leave every mock and live test green. It also
// exercises the read path end to end -- actors.roles and editable have to
// survive the JSON decode to reach a diagnostic at all.
func TestPolicyImportStateDiagnostics(t *testing.T) {
	t.Parallel()

	importWith := func(t *testing.T, aspect string) *resource.ImportStateResponse {
		t.Helper()
		body := `{
		  "urn": "urn:li:dataHubPolicy:admin-platform-policy",
		  "dataHubPolicyKey": {"value": {"id": "admin-platform-policy"}},
		  "dataHubPolicyInfo": {"value": ` + aspect + `}
		}`

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}))
		t.Cleanup(srv.Close)

		client, err := datahub.NewClient(srv.URL, "test-token")
		if err != nil {
			t.Fatalf("building client: %v", err)
		}

		ctx := context.Background()
		r := &policyResource{client: client}

		schemaResp := &resource.SchemaResponse{}
		r.Schema(ctx, resource.SchemaRequest{}, schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Fatalf("schema: %v", schemaResp.Diagnostics)
		}

		resp := &resource.ImportStateResponse{
			State: tfsdk.State{
				Schema: schemaResp.Schema,
				Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
			},
		}
		r.ImportState(ctx, resource.ImportStateRequest{ID: "admin-platform-policy"}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("import failed, but importing must stay possible: %v", resp.Diagnostics)
		}
		return resp
	}

	t.Run("role_bearing_non_editable_policy_warns_twice", func(t *testing.T) {
		t.Parallel()
		resp := importWith(t, `{
		    "displayName": "Admins - Platform Policy",
		    "description": "Grants all platform privileges to Admin role holders.",
		    "type": "PLATFORM",
		    "state": "ACTIVE",
		    "privileges": ["MANAGE_POLICIES"],
		    "actors": {
		      "users": [], "groups": [], "allUsers": false, "allGroups": false,
		      "resourceOwners": false, "resourceOwnersTypes": [],
		      "roles": ["urn:li:dataHubRole:Admin"]
		    },
		    "editable": false
		  }`)

		// Roles and the non-editable marker, which is how eight of DataHub's
		// nine role-bearing bootstrap policies are shipped.
		if errs, warns := severities(resp.Diagnostics); errs != 0 || warns != 2 {
			t.Fatalf("got %d errors and %d warnings, want 0 and 2: %v", errs, warns, resp.Diagnostics)
		}
		joined := resp.Diagnostics[0].Detail() + resp.Diagnostics[1].Detail()
		for _, want := range []string{"urn:li:dataHubRole:Admin", "OSS-1216", "editable = false"} {
			if !strings.Contains(joined, want) {
				t.Errorf("import diagnostics are missing %q:\n%s", want, joined)
			}
		}
	})

	t.Run("ordinary_policy_imports_silently", func(t *testing.T) {
		t.Parallel()
		// No roles, and no editable field at all -- the shape of every policy
		// this provider has ever written, since PolicyUpdateInput cannot set
		// editable and the PDL defaults it to true. Reading an absent editable
		// as false would put the non-editable warning on every single import,
		// and because it is only a warning nothing else in the suite would fail.
		resp := importWith(t, `{
		    "displayName": "Ordinary Policy",
		    "description": "",
		    "type": "PLATFORM",
		    "state": "ACTIVE",
		    "privileges": ["MANAGE_POLICIES"],
		    "actors": {
		      "users": ["urn:li:corpuser:datahub"], "groups": [],
		      "allUsers": false, "allGroups": false,
		      "resourceOwners": false, "resourceOwnersTypes": []
		    }
		  }`)

		if len(resp.Diagnostics) != 0 {
			t.Fatalf("ordinary import produced diagnostics: %v", resp.Diagnostics)
		}
	})
}
