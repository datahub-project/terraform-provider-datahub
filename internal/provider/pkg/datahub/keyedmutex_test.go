// Copyright 2026 The DataHub Project Authors
// SPDX-License-Identifier: Apache-2.0

package datahub

import (
	"sync"
	"testing"
	"time"
)

// TestKeyedMutex_SameKeySerializes asserts that holders of the same key never
// overlap (mutual exclusion). Run with -race to also catch data races.
func TestKeyedMutex_SameKeySerializes(t *testing.T) {
	var km keyedMutex
	var track sync.Mutex
	active, maxActive := 0, 0

	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := km.lock("same")
			defer unlock()

			track.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			track.Unlock()

			time.Sleep(time.Millisecond)

			track.Lock()
			active--
			track.Unlock()
		}()
	}
	wg.Wait()

	if maxActive != 1 {
		t.Fatalf("same key allowed %d concurrent holders, want 1", maxActive)
	}
}

// TestKeyedMutex_DifferentKeysDoNotBlock asserts a held key does not block the
// acquisition of a different key (per-key, not global, serialization).
func TestKeyedMutex_DifferentKeysDoNotBlock(t *testing.T) {
	var km keyedMutex

	unlockA := km.lock("a")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		unlockB := km.lock("b") // must not block while key "a" is held
		unlockB()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(`acquiring key "b" blocked while key "a" was held`)
	}
}
