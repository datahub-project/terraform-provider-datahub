// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "errors"

// ErrNotFound is returned by client GET and DELETE methods when the DataHub
// API responds with HTTP 404. Callers use errors.Is to distinguish a missing
// entity from other API failures.
var ErrNotFound = errors.New("not found")
