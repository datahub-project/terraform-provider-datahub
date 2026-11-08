// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahubtesting

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/datahub-project/terraform-provider-datahub/internal/provider/pkg/datahub"
)

// CleanupSupersededOAuthSecrets registers a t.Cleanup that deletes every
// dataHubSecret DataHub minted for the OAuth authorization server serverID,
// once the test (including its destroy) has finished. Live targets only.
//
// DataHub stores an OAuth server's client secret as a separate dataHubSecret
// named "<serverID>_clientSecret_<random UUID>". Rotating the secret mints a
// new one and clearing it drops the reference, but in neither case does the
// server delete the secret it stopped using, and deleting the server removes
// only the secret it still points to. A test that rotates and then clears
// therefore leaves two secrets behind on every run, which piles up on a shared
// instance. The resource documents this as an orphan trail it cannot clean up;
// the test can, because it knows the server id it used.
//
// The count is logged, not asserted: if DataHub starts deleting superseded
// secrets itself, the log shows zero and nothing fails.
func (tg *Target) CleanupSupersededOAuthSecrets(t *testing.T, serverID string) {
	t.Helper()
	if tg.Kind == TargetMock {
		return
	}
	t.Cleanup(func() {
		client, err := datahub.NewClient(os.Getenv("DATAHUB_GMS_URL"), os.Getenv("DATAHUB_GMS_TOKEN"))
		if err != nil {
			t.Logf("CleanupSupersededOAuthSecrets: creating client: %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		urns, err := client.ListSecretURNs(ctx)
		if err != nil {
			t.Logf("CleanupSupersededOAuthSecrets: listing secrets: %v", err)
			return
		}
		prefix := "urn:li:dataHubSecret:" + serverID + "_clientSecret_"
		deleted := 0
		for _, urn := range urns {
			if !strings.HasPrefix(urn, prefix) {
				continue
			}
			if err := client.DeleteSecret(ctx, urn); err != nil {
				t.Logf("CleanupSupersededOAuthSecrets: deleting %s: %v", urn, err)
				continue
			}
			deleted++
		}
		t.Logf("CleanupSupersededOAuthSecrets: deleted %d client secret(s) left behind for %s", deleted, serverID)
	})
}
