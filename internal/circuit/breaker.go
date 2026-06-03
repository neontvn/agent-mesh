/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package circuit implements a per-peer circuit breaker used by the
// sidecar's outbound A2A path. After a threshold of consecutive failures
// to a peer, the circuit "opens" — subsequent calls to that peer are
// short-circuited (callers should skip and try a different peer). After
// the cooldown elapses, the breaker transitions to "half-open" on the
// next call: a single trial is allowed through; success closes the
// circuit, another failure reopens it.
package circuit

import (
	"sync"
	"time"
)

// Breaker is a per-peer circuit breaker. It is safe for concurrent use.
//
// State transitions per peer:
//
//	closed   --(threshold consecutive failures)-->   open
//	open     --(cooldown elapsed)-->                 half-open
//	half-open --(next call succeeds)-->              closed
//	half-open --(next call fails)-->                 open
type Breaker struct {
	mu       sync.Mutex
	failures map[string]int       // peer -> consecutive failure count
	openedAt map[string]time.Time // peer -> time circuit opened (zero if closed)

	threshold int           // failures required to open
	cooldown  time.Duration // duration to stay open before half-open
}

// New constructs a Breaker with the given failure threshold and cooldown
// duration. Sensible defaults for a dev environment: threshold=3,
// cooldown=15*time.Second.
func New(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{
		failures:  map[string]int{},
		openedAt:  map[string]time.Time{},
		threshold: threshold,
		cooldown:  cooldown,
	}
}

// IsOpen reports whether the circuit for the given peer is currently open
// (the caller should skip this peer). If the cooldown has elapsed, the
// open mark is cleared as a side effect, transitioning the circuit to a
// "half-open" state where the next call is allowed through as a trial.
func (b *Breaker) IsOpen(peer string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	openedAt, open := b.openedAt[peer]
	if !open {
		return false
	}
	if time.Since(openedAt) > b.cooldown {
		// Cooldown has elapsed — clear the mark; next call is a trial.
		delete(b.openedAt, peer)
		return false
	}
	return true
}

// RecordSuccess clears any accumulated failure count and any open mark
// for the peer. Called after a successful call.
func (b *Breaker) RecordSuccess(peer string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.failures, peer)
	delete(b.openedAt, peer)
}

// RecordFailure increments the failure count for the peer. If the count
// reaches the threshold and the circuit is not already open, it opens
// the circuit and returns true. Returns false on every other call.
func (b *Breaker) RecordFailure(peer string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures[peer]++
	if b.failures[peer] < b.threshold {
		return false
	}
	if _, already := b.openedAt[peer]; already {
		return false
	}
	b.openedAt[peer] = time.Now()
	return true
}
