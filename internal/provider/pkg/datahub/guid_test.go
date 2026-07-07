// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "testing"

// Ground-truth values produced by the DataHub Python SDK's datahub_guid():
//
//	md5(json.dumps(obj, separators=(",",":"), sort_keys=True))
//
// If these ever drift, the provider would derive contract URNs that do not
// match SDK-created entities, silently producing duplicate contracts.
func TestGUID(t *testing.T) {
	tests := []struct {
		name string
		obj  any
		want string
	}{
		{
			name: "entity dict matches SDK contract id",
			obj:  map[string]string{"entity": "urn:li:dataset:(urn:li:dataPlatform:postgres,mydb.public.orders,PROD)"},
			want: "de9ff15b4d1545e318da79d38ae05d10",
		},
		{
			name: "keys are sorted regardless of insertion order",
			obj:  map[string]string{"b": "2", "a": "1"},
			want: "8018d630c38e45a64531824279891103",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GUID(tt.obj)
			if err != nil {
				t.Fatalf("GUID returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("GUID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDataContractID(t *testing.T) {
	const datasetURN = "urn:li:dataset:(urn:li:dataPlatform:postgres,mydb.public.orders,PROD)"
	const want = "de9ff15b4d1545e318da79d38ae05d10"

	got, err := DataContractID(datasetURN)
	if err != nil {
		t.Fatalf("DataContractID returned error: %v", err)
	}
	if got != want {
		t.Errorf("DataContractID = %q, want %q", got, want)
	}
}
