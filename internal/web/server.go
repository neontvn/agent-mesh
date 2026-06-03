/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package web

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

//go:embed all:static
var embeddedFS embed.FS

// upgrader transitions an HTTP connection to a WebSocket. CheckOrigin is
// permissive because the UI is intended for local development; production
// deployment would tighten this.
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server hosts the visualization UI (static files) and the live event stream
// (WebSocket).
type Server struct {
	Bus *EventBus
}

// NewServer constructs a Server backed by the given event bus.
func NewServer(bus *EventBus) *Server {
	return &Server{Bus: bus}
}

// Handler returns the HTTP handler. Static files are served at "/", the
// WebSocket endpoint at "/ws".
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	staticFS, err := fs.Sub(embeddedFS, "static")
	if err != nil {
		log.Fatalf("embed sub: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	mux.HandleFunc("/ws", s.handleWS)
	return mux
}

// handleWS upgrades to a WebSocket and forwards every event published on
// the bus until the client disconnects.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	sub := s.Bus.Subscribe()
	defer s.Bus.Unsubscribe(sub)

	for event := range sub {
		data, err := event.JSON()
		if err != nil {
			log.Printf("event encode: %v", err)
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}
