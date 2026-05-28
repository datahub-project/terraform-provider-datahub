// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package extracttool

import (
	"testing"
)

func TestLabelFromURN(t *testing.T) {
	tests := []struct {
		urn  string
		want string
	}{
		{
			urn:  "urn:li:dataHubSecret:DBT_CLOUD_SECRET_FICTION_BANK",
			want: "dbt_cloud_secret_fiction_bank",
		},
		{
			urn:  "urn:li:dataHubConnection:da45c888-ef22-4a16-8a4e-85c0ee539c80",
			want: "da45c888_ef22_4a16_8a4e_85c0ee539c80", // starts with 'd', no r_ prefix needed
		},
		{
			urn:  "urn:li:dataHubIngestionSource:03001a84-1213-4f51-abcf-181d489c38b6",
			want: "r_03001a84_1213_4f51_abcf_181d489c38b6",
		},
		{
			urn:  "urn:li:dataHubIngestionSource:my-bigquery-source",
			want: "my_bigquery_source",
		},
		{
			urn:  "urn:li:dataHubSecret:__system_teams-0",
			want: "system_teams_0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.urn, func(t *testing.T) {
			got := LabelFromURN(tc.urn)
			if got != tc.want {
				t.Errorf("LabelFromURN(%q) = %q; want %q", tc.urn, got, tc.want)
			}
		})
	}
}

func TestUniqueLabels(t *testing.T) {
	urns := []string{
		"urn:li:dataHubSecret:MY_SECRET",
		"urn:li:dataHubSecret:MY_SECRET", // duplicate
		"urn:li:dataHubSecret:OTHER",
	}
	got := uniqueLabels(urns)
	if got[0] != "my_secret" {
		t.Errorf("first label = %q; want my_secret", got[0])
	}
	if got[1] != "my_secret_2" {
		t.Errorf("second label = %q; want my_secret_2", got[1])
	}
	if got[2] != "other" {
		t.Errorf("third label = %q; want other", got[2])
	}
}
