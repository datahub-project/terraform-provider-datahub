// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/datahubtesting"
	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// These tests run only against a live DataHub. They prove, on the real
// server, that a write the provider makes after reading an aspect cannot
// overwrite a change made to that aspect in between, and (the control arm)
// that without the precondition it does. The competing write is injected by
// an intercepting proxy at the exact moment, so nothing here depends on
// timing.

// outOfBandWriter edits an aspect the way another producer would: a correct
// conditional read-modify-write straight to the upstream, not through the
// proxy, setting one field the provider does not manage.
type outOfBandWriter struct {
	t      *testing.T
	client *datahub.Client
}

func newOutOfBandWriter(t *testing.T) *outOfBandWriter {
	t.Helper()
	c, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	return &outOfBandWriter{t: t, client: c}
}

func (o *outOfBandWriter) do(method, path string, body any) (int, []byte) {
	o.t.Helper()
	req, err := o.client.NewRequest(context.Background(), method, path, body)
	if err != nil {
		o.t.Fatal(err)
	}
	res, err := o.client.Do(req)
	if err != nil {
		o.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, out
}

// read returns the aspect's value and version.
func (o *outOfBandWriter) read(entityPath, urn, aspect string) (map[string]any, string) {
	o.t.Helper()
	status, body := o.do(http.MethodGet, fmt.Sprintf("/openapi/v3/entity/%s/%s?aspects=%s&systemMetadata=true", entityPath, url.PathEscape(urn), aspect), nil)
	if status != http.StatusOK {
		o.t.Fatalf("reading %s on %s: %d %s", aspect, urn, status, body)
	}
	var entity map[string]json.RawMessage
	if err := json.Unmarshal(body, &entity); err != nil {
		o.t.Fatal(err)
	}
	var a struct {
		Value          map[string]any `json:"value"`
		SystemMetadata struct {
			Version string `json:"version"`
		} `json:"systemMetadata"`
	}
	dec := json.NewDecoder(bytes.NewReader(entity[aspect]))
	dec.UseNumber()
	if err := dec.Decode(&a); err != nil {
		o.t.Fatal(err)
	}
	return a.Value, a.SystemMetadata.Version
}

// set writes field=value on the aspect, conditionally on the version read.
func (o *outOfBandWriter) set(entityPath, urn, aspect, field string, value any) {
	o.t.Helper()
	current, version := o.read(entityPath, urn, aspect)
	current[field] = value
	status, body := o.do(http.MethodPost, "/openapi/v3/entity/"+entityPath+"?async=false", []map[string]any{{
		"urn":  urn,
		aspect: map[string]any{"value": current, "headers": map[string]string{"If-Version-Match": version}},
	}})
	if status != http.StatusOK {
		o.t.Fatalf("out-of-band write of %s.%s on %s: %d %s", aspect, field, urn, status, body)
	}
}

func proxyStatuses(posts []datahubtesting.ProxyPost, urn, aspect string) []int {
	var out []int
	for _, p := range posts {
		if p.URN == urn && p.Aspect == aspect {
			out = append(out, p.Status)
		}
	}
	return out
}

// TestLiveConditionalWriteRace drives the client directly: create a user,
// arm a competing edit of corpUserInfo for the moment after the client's
// read-for-write, update the user, and check both edits survive. The
// control arm strips the precondition and checks the competing edit is lost,
// which is the standing proof that this harness detects the hazard.
func TestLiveConditionalWriteRace(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if !tg.IsLive() {
		t.Skip("the race needs a real DataHub; the mock cannot lose one")
	}
	upstream := os.Getenv("DATAHUB_GMS_URL")
	proxy := datahubtesting.NewInterceptingProxy(t, upstream)
	c, err := datahub.NewClient(proxy.URL, os.Getenv("DATAHUB_GMS_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	oob := newOutOfBandWriter(t)
	ctx := context.Background()

	username := tg.Name("tfprovider-race")
	urn := "urn:li:corpuser:" + username
	if _, err := c.UpsertCorpUser(ctx, datahub.UpsertCorpUserInput{Username: username, DisplayName: "Race", Title: "one"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteUser(ctx, urn) })

	t.Run("precondition_keeps_both_edits", func(t *testing.T) {
		proxy.OnFirstGet(urn, "corpUserInfo", func() { oob.set("corpuser", urn, "corpUserInfo", "countryCode", "AU") })
		if _, err := c.UpsertCorpUser(ctx, datahub.UpsertCorpUserInput{Username: username, DisplayName: "Race", Title: "two"}); err != nil {
			t.Fatalf("update: %v", err)
		}
		info, _ := oob.read("corpuser", urn, "corpUserInfo")
		if info["title"] != "two" || info["countryCode"] != "AU" {
			t.Errorf("after a conditional write that raced a competing edit: title=%v countryCode=%v, want two and AU", info["title"], info["countryCode"])
		}
		statuses := proxyStatuses(proxy.Posts(), urn, "corpUserInfo")
		if len(statuses) < 3 || statuses[len(statuses)-2] != http.StatusPreconditionFailed || statuses[len(statuses)-1] != http.StatusOK {
			t.Errorf("proxy saw corpUserInfo POST statuses %v; want the update to meet a 412 and retry to 200", statuses)
		}
	})

	t.Run("control_without_precondition_loses_the_edit", func(t *testing.T) {
		proxy.StripVersionHeaders()
		proxy.OnFirstGet(urn, "corpUserInfo", func() { oob.set("corpuser", urn, "corpUserInfo", "countryCode", "NZ") })
		if _, err := c.UpsertCorpUser(ctx, datahub.UpsertCorpUserInput{Username: username, DisplayName: "Race", Title: "three"}); err != nil {
			t.Fatalf("update without precondition: %v", err)
		}
		info, _ := oob.read("corpuser", urn, "corpUserInfo")
		if info["title"] != "three" {
			t.Errorf("the unconditional write did not land: title=%v", info["title"])
		}
		if info["countryCode"] == "NZ" {
			t.Errorf("the competing edit survived a write without If-Version-Match; the server now merges, or the proxy did not strip the header, and the precondition tests above no longer prove anything")
		}
		statuses := proxyStatuses(proxy.Posts(), urn, "corpUserInfo")
		if statuses[len(statuses)-1] != http.StatusOK {
			t.Errorf("control write status %d, want 200 (the lost update is silent)", statuses[len(statuses)-1])
		}
	})
}

// TestAcc_LiveConditionalWriteSurvivesConcurrentEdit does the same through
// the provider itself: Terraform's own client goes through the proxy, and a
// competing edit lands between the resource's read-for-write and its write.
func TestAcc_LiveConditionalWriteSurvivesConcurrentEdit(t *testing.T) {
	tg := datahubtesting.SetupTarget(t)
	if !tg.IsLive() {
		t.Skip("the race needs a real DataHub; the mock cannot lose one")
	}
	upstream := os.Getenv("DATAHUB_GMS_URL")
	oob := newOutOfBandWriter(t)
	proxy := datahubtesting.NewInterceptingProxy(t, upstream)
	t.Setenv("DATAHUB_GMS_URL", proxy.URL)

	username := tg.Name("tfprovider-acc-race")
	urn := "urn:li:corpuser:" + username
	config := func(title string) string {
		return fmt.Sprintf(`
provider "datahub" {}

resource "datahub_corp_user" "race" {
  username     = %q
  display_name = "Acceptance Race"
  title        = %q
}
`, username, title)
	}
	checkInfo := func(wantTitle, wantCountry string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			info, _ := oob.read("corpuser", urn, "corpUserInfo")
			if info["title"] != wantTitle || info["countryCode"] != wantCountry {
				return fmt.Errorf("corpUserInfo title=%v countryCode=%v, want %q and %q", info["title"], info["countryCode"], wantTitle, wantCountry)
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("one")},
			{
				PreConfig: func() {
					proxy.OnFirstGet(urn, "corpUserInfo", func() { oob.set("corpuser", urn, "corpUserInfo", "countryCode", "AU") })
				},
				Config: config("two"),
				Check: resource.ComposeTestCheckFunc(
					checkInfo("two", "AU"),
					func(*terraform.State) error {
						statuses := proxyStatuses(proxy.Posts(), urn, "corpUserInfo")
						if len(statuses) < 3 || statuses[len(statuses)-2] != http.StatusPreconditionFailed || statuses[len(statuses)-1] != http.StatusOK {
							return fmt.Errorf("proxy saw corpUserInfo POST statuses %v; want the update to meet a 412 and retry to 200", statuses)
						}
						return nil
					},
				),
			},
		},
	})
}
