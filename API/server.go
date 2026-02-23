package API

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
	http.HandleFunc("/health", s.handleHealth)
	http.HandleFunc("/receipt", s.handleReceipt)

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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {

	status := map[string]interface{}{
		"status":      "ok",
		"event_count": s.Engine.EventCount(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleReceipt(w http.ResponseWriter, r *http.Request) {

	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "invalid id", 400)
		return
	}

	pdfBytes, err := s.Engine.GenerateReceipt(id)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=receipt.pdf")
	w.Write(pdfBytes)
}
