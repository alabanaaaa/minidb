package API

import (
	"encoding/json"
	"mini-database/core"
	"mini-database/engine"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	Engine *engine.Engine
}

var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mini_http_requests_total",
			Help: "Total HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mini_http_request_duration_seconds",
			Help:    "HTTP request durations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	salesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mini_sales_total",
			Help: "Total sales recorded",
		},
	)

	eventsReplicatedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "mini_events_replicated_total",
			Help: "Total events replicated",
		},
	)
	eventCountGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "mini_event_count",
			Help: "Current number of events in memory",
		},
	)
)

func init() {
	prometheus.MustRegister(requestsTotal, requestDuration, salesTotal, eventsReplicatedTotal, eventCountGauge)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) Start(addr string) error {

	http.HandleFunc("/events", s.handleGetEvents)
	http.HandleFunc("/replicate", s.handleReplicate)
	http.HandleFunc("/sale", s.handleSale)
	http.HandleFunc("/health", s.handleHealth)
	http.HandleFunc("/receipt", s.handleReceipt)
	http.Handle("/metrics", promhttp.Handler())

	// Periodically update the event count gauge so Prometheus can scrape it
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if s != nil && s.Engine != nil {
				eventCountGauge.Set(float64(s.Engine.EventCount()))
			}
		}
	}()

	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {

	rec := &statusRecorder{ResponseWriter: w, status: 200}
	start := time.Now()
	defer func() {
		requestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(rec.status)).Inc()
	}()

	afterStr := r.URL.Query().Get("after")
	after, _ := strconv.Atoi(afterStr)

	events := s.Engine.EventsAfter(after)

	json.NewEncoder(rec).Encode(events)
}

func (s *Server) handleReplicate(w http.ResponseWriter, r *http.Request) {

	rec := &statusRecorder{ResponseWriter: w, status: 200}
	start := time.Now()
	defer func() {
		requestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(rec.status)).Inc()
	}()

	var incoming []engine.Event

	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(rec, err.Error(), 400)
		return
	}

	for _, evt := range incoming {
		if err := s.Engine.AppendReplicatedEvent(evt); err != nil {
			http.Error(rec, err.Error(), 500)
			return
		}
		eventsReplicatedTotal.Inc()
	}

	rec.WriteHeader(http.StatusOK)
}

func (s *Server) handleSale(w http.ResponseWriter, r *http.Request) {

	rec := &statusRecorder{ResponseWriter: w, status: 200}
	start := time.Now()
	defer func() {
		requestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(rec.status)).Inc()
	}()

	var sale core.Sale

	if err := json.NewDecoder(r.Body).Decode(&sale); err != nil {
		http.Error(rec, err.Error(), 400)
		return
	}

	if err := s.Engine.RecordSale(sale); err != nil {
		http.Error(rec, err.Error(), 400)
		return
	}

	salesTotal.Inc()
	rec.WriteHeader(http.StatusOK)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {

	rec := &statusRecorder{ResponseWriter: w, status: 200}
	start := time.Now()
	defer func() {
		requestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(rec.status)).Inc()
	}()

	status := map[string]interface{}{
		"status":      "ok",
		"event_count": s.Engine.EventCount(),
	}

	rec.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rec).Encode(status)
}

func (s *Server) handleReceipt(w http.ResponseWriter, r *http.Request) {

	rec := &statusRecorder{ResponseWriter: w, status: 200}
	start := time.Now()
	defer func() {
		requestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(time.Since(start).Seconds())
		requestsTotal.WithLabelValues(r.Method, r.URL.Path, strconv.Itoa(rec.status)).Inc()
	}()

	idStr := r.URL.Query().Get("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(rec, "invalid id", 400)
		return
	}

	pdfBytes, err := s.Engine.GenerateReceipt(id)
	if err != nil {
		http.Error(rec, err.Error(), 400)
		return
	}

	rec.Header().Set("Content-Type", "application/pdf")
	rec.Header().Set("Content-Disposition", "attachment; filename=receipt.pdf")
	rec.Write(pdfBytes)
}
