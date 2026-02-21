package api

import (
	"encoding/json"
	"mini-database/core"
	"mini-database/engine"
	"net/http"
	"strconv"
)

type Server struct {
	Engine *engine.Engine
}

func (s *Server) Start(addr string) error {

	http.HandleFunc("/events", s.handleGetEvents)
	http.HandleFunc("/replicate", s.handleReplicate)
	http.HandleFunc("/sale", s.handleSale)

	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {

	afterStr := r.URL.Query().Get("after")
	after, _ := strconv.Atoi(afterStr)

	events := s.Engine.EventsAfter(after)

	json.NewEncoder(w).Encode(events)
}

func (s *Server) handleReplicate(w http.ResponseWriter, r *http.Request) {

	var incoming []engine.Event

	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	for _, evt := range incoming {
		if err := s.Engine.AppendReplicatedEvent(evt); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSale(w http.ResponseWriter, r *http.Request) {

	var sale core.Sale

	if err := json.NewDecoder(r.Body).Decode(&sale); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if err := s.Engine.RecordSale(sale); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	w.WriteHeader(http.StatusOK)
}
