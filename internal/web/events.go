/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package web

import (
	"encoding/json"
	"sync"
	"time"
)

// EventType identifies a category of mesh event broadcast to UI subscribers.
type EventType string

const (
	EventAgentRegistered    EventType = "agent_registered"
	EventAgentUnregistered  EventType = "agent_unregistered"
	EventAgentHeartbeat     EventType = "agent_heartbeat"
	EventAgentHealthChanged EventType = "agent_health_changed"
	EventInvokeCompleted    EventType = "invoke_completed"
	EventTaskUpdated        EventType = "task_updated"
)

// Event is a single mesh event broadcast to all WebSocket subscribers.
type Event struct {
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// JSON encodes the event for transmission over the WebSocket.
func (e Event) JSON() ([]byte, error) {
	return json.Marshal(e)
}

// EventBus is a fan-out broadcaster for mesh events. Publishers (the gRPC
// server, the controller) call Publish; subscribers (each WebSocket client)
// receive events on a channel.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

// NewEventBus constructs an empty EventBus.
func NewEventBus() *EventBus {
	return &EventBus{subscribers: map[chan Event]struct{}{}}
}

// Subscribe returns a buffered channel that receives every published event
// until Unsubscribe is called.
func (b *EventBus) Subscribe() chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 64)
	b.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe detaches a subscriber and closes its channel.
func (b *EventBus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subscribers[ch]; !ok {
		return
	}
	delete(b.subscribers, ch)
	close(ch)
}

// Publish broadcasts an event to every subscriber. Slow subscribers (channel
// full) drop the event rather than blocking publishers — visualization is a
// best-effort concern, not a correctness concern.
func (b *EventBus) Publish(t EventType, data map[string]interface{}) {
	event := Event{
		Type:      t,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// drop on slow subscriber
		}
	}
}
