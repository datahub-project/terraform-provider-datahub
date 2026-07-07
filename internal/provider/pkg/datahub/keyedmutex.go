// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import "sync"

// keyedMutex provides mutual exclusion scoped to a string key: operations that
// hold the same key are serialized, while operations holding different keys
// proceed concurrently. The zero value is ready to use.
//
// This is the minimal, standard "mutex-per-key" pattern -- equivalent to the
// SDKv2 helper/mutexkv.MutexKV, which terraform-plugin-framework does not
// provide -- and is used here to serialize structured-property writes per target
// entity (CAT-2568 workaround; see Client.lockEntityStructuredProps).
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// lock acquires the mutex for key and returns a function that releases it.
// Typical use: unlock := k.lock(key); defer unlock().
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[string]*sync.Mutex)
	}
	l := k.locks[key]
	if l == nil {
		l = &sync.Mutex{}
		k.locks[key] = l
	}
	k.mu.Unlock()

	l.Lock()
	return l.Unlock
}
