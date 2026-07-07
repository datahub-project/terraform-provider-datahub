// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// GUID reproduces the DataHub Python SDK's datahub_guid() so that URNs derived
// here match those the `datahub` CLI/SDK would produce for the same input. The
// SDK computes it as:
//
//	md5(json.dumps(obj, separators=(",", ":"), sort_keys=True))
//
// i.e. a compact, key-sorted JSON serialization hashed with MD5 and rendered as
// a lowercase hex digest.
//
// Parity notes:
//   - Pass a map (not a struct) for any multi-key object: encoding/json sorts
//     map keys but preserves struct field declaration order, whereas the SDK
//     sorts all keys. Single-field objects are unaffected either way.
//   - HTML escaping is disabled so characters common in URNs (`<`, `>`, `&`)
//     serialize verbatim, matching Python's json.dumps default.
func GUID(obj any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return "", fmt.Errorf("serializing object for GUID: %w", err)
	}
	// json.Encoder.Encode appends a trailing newline that json.dumps does not.
	key := bytes.TrimRight(buf.Bytes(), "\n")

	sum := md5.Sum(key)
	return hex.EncodeToString(sum[:]), nil
}

// DataContractID derives the deterministic data contract id for a dataset,
// matching the DataHub Python SDK's stable contract URN derivation
// (datahub_guid({"entity": <dataset_urn>})). Using this id ensures a
// Terraform-managed contract and an SDK-created contract for the same dataset
// resolve to the same entity rather than two competing contracts, and gives the
// provider a URN it knows before the upsert returns.
//
// The returned value is the id portion only; callers prepend
// DataContractURNPrefix to form the full URN.
func DataContractID(datasetURN string) (string, error) {
	return GUID(map[string]string{"entity": datasetURN})
}
