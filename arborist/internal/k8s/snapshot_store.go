// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package k8s

import (
	"sync"
	"time"

	"github.com/ai-dynamo/grove/arborist/internal/clusterstate"
)

const (
	// debounceQuietWindow is how long to wait after the last change event
	// before rebuilding the snapshot.
	debounceQuietWindow = 250 * time.Millisecond
	// debounceMaxWait is the maximum time from the first event in a burst
	// before a rebuild is forced, even if events are still arriving.
	debounceMaxWait = 500 * time.Millisecond
)

// debouncer coalesces rapid calls into a single delayed execution using a
// two-parameter strategy: a quiet timer that resets on each call, and a max
// timer that caps how long a burst can delay the callback.
type debouncer struct {
	quietInterval time.Duration
	maxInterval   time.Duration
	mu            sync.Mutex
	quietTimer    *time.Timer
	maxTimer      *time.Timer
}

// schedule resets the quiet timer on every call. On the first call in a burst
// (no max timer running), it also starts the max timer. When either timer
// fires, the callback executes and both timers are cancelled.
func (d *debouncer) schedule(fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fire := func() {
		d.mu.Lock()
		// If both timers are already nil, another fire already ran — skip.
		if d.quietTimer == nil && d.maxTimer == nil {
			d.mu.Unlock()
			return
		}
		// Cancel whichever timer didn't fire.
		if d.quietTimer != nil {
			d.quietTimer.Stop()
			d.quietTimer = nil
		}
		if d.maxTimer != nil {
			d.maxTimer.Stop()
			d.maxTimer = nil
		}
		d.mu.Unlock()
		fn()
	}

	if d.quietTimer != nil {
		d.quietTimer.Stop()
	}
	d.quietTimer = time.AfterFunc(d.quietInterval, fire)

	// First event in a burst — start the max-wait timer.
	if d.maxTimer == nil {
		d.maxTimer = time.AfterFunc(d.maxInterval, fire)
	}
}

// stop cancels any pending callbacks.
func (d *debouncer) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.quietTimer != nil {
		d.quietTimer.Stop()
		d.quietTimer = nil
	}
	if d.maxTimer != nil {
		d.maxTimer.Stop()
		d.maxTimer = nil
	}
}

// snapshotStore holds the snapshot state, mutex, notification channel, and
// debounce logic. It is embedded in InformerGlobalCache.
//
// Concurrency design: Two separate mutexes are used for distinct concerns:
//   - mu (sync.RWMutex): Guards the snapshot pointer and the updates channel.
//     RWMutex is chosen because reads (Snapshot()) are far more frequent than
//     writes (storeAndNotify()), and read-heavy workloads benefit from shared
//     read locks.
//   - debounce.mu (sync.Mutex): Serializes timer operations within the debouncer.
//     This is independent of snapshot access — it only protects the timer.
//
// The two locks are never held simultaneously, so there is no deadlock risk.
type snapshotStore struct {
	snapshot  *clusterstate.CacheSnapshot
	mu        sync.RWMutex
	updatesCh chan struct{}
	debounce  debouncer
	stopped   bool
}

// newSnapshotStore creates a snapshotStore with a buffered updates channel.
func newSnapshotStore() snapshotStore {
	return snapshotStore{
		updatesCh: make(chan struct{}, 1),
		debounce:  debouncer{quietInterval: debounceQuietWindow, maxInterval: debounceMaxWait},
	}
}

// Snapshot returns the latest CacheSnapshot. Thread-safe.
func (s *snapshotStore) Snapshot() *clusterstate.CacheSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

// Updates returns the notification channel, lazily initializing it if needed
// so the zero value of snapshotStore is safe to use.
func (s *snapshotStore) Updates() <-chan struct{} {
	s.mu.Lock()
	if s.updatesCh == nil {
		s.updatesCh = make(chan struct{}, 1)
	}
	ch := s.updatesCh
	s.mu.Unlock()
	return ch
}

// storeAndNotify writes a new snapshot and sends a non-blocking notification.
// The send is inside the lock so that the stopped check and channel send are
// atomic with respect to stop() closing the channel.
func (s *snapshotStore) storeAndNotify(snapshot *clusterstate.CacheSnapshot) {
	s.mu.Lock()
	s.snapshot = snapshot
	if !s.stopped && s.updatesCh != nil {
		select {
		case s.updatesCh <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
}

// scheduleRebuild debounces calls to rebuildFn using a two-parameter strategy:
// the callback fires after debounceQuietWindow of inactivity, or after
// debounceMaxWait from the first event in a burst, whichever comes first.
func (s *snapshotStore) scheduleRebuild(rebuildFn func()) {
	s.debounce.schedule(rebuildFn)
}

// stop stops the debounce timer, marks as stopped, and closes the updates channel.
func (s *snapshotStore) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	s.debounce.stop()
	if s.updatesCh != nil {
		close(s.updatesCh)
	}
}
