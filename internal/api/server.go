package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"mini-database/engine"
	"mini-database/internal/auth"
	"mini-database/internal/config"
	"mini-database/internal/db"
	"mini-database/internal/mpesa"
	"mini-database/internal/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Metrics struct {
	mu          sync.Mutex
	requests    map[string]int64
	errors      map[string]int64
	durations   map[string]time.Duration
	startTime   time.Time
	activeConns int64
	totalConns  int64
}

func NewMetrics() *Metrics {
	return &Metrics{
		requests:  make(map[string]int64),
		errors:    make(map[string]int64),
		durations: make(map[string]time.Duration),
		startTime: time.Now(),
	}
}

func (m *Metrics) Record(method, path string, status int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := method + " " + path
	m.requests[key]++
	m.durations[key] += duration
	if status >= 400 {
		m.errors[key]++
	}
}

func (m *Metrics) IncActiveConns() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeConns++
	m.totalConns++
}

func (m *Metrics) DecActiveConns() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeConns--
}

func (m *Metrics) PrometheusOutput() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	uptime := time.Since(m.startTime).Seconds()
	var sb strings.Builder
	sb.WriteString("# HELP http_requests_total Total HTTP requests\n")
	sb.WriteString("# TYPE http_requests_total counter\n")
	for key, count := range m.requests {
		sb.WriteString(fmt.Sprintf("http_requests_total{path=\"%s\"} %d\n", key, count))
	}
	sb.WriteString("# HELP http_errors_total Total HTTP errors (4xx/5xx)\n")
	sb.WriteString("# TYPE http_errors_total counter\n")
	for key, count := range m.errors {
		sb.WriteString(fmt.Sprintf("http_errors_total{path=\"%s\"} %d\n", key, count))
	}
	sb.WriteString("# HELP http_duration_seconds_total Total request duration\n")
	sb.WriteString("# TYPE http_duration_seconds_total counter\n")
	for key, dur := range m.durations {
		sb.WriteString(fmt.Sprintf("http_duration_seconds_total{path=\"%s\"} %.3f\n", key, dur.Seconds()))
	}
	sb.WriteString("# HELP app_uptime_seconds Application uptime\n")
	sb.WriteString("# TYPE app_uptime_seconds gauge\n")
	sb.WriteString(fmt.Sprintf("app_uptime_seconds %.0f\n", uptime))
	sb.WriteString("# HELP http_connections_active Active HTTP connections\n")
	sb.WriteString("# TYPE http_connections_active gauge\n")
	sb.WriteString(fmt.Sprintf("http_connections_active %d\n", m.activeConns))
	sb.WriteString("# HELP http_connections_total Total HTTP connections\n")
	sb.WriteString("# TYPE http_connections_total counter\n")
	sb.WriteString(fmt.Sprintf("http_connections_total %d\n", m.totalConns))
	return sb.String()
}

type Server struct {
	router       *chi.Mux
	db           *db.DB
	engine       *engine.Engine
	auth         *auth.Service
	cfg          *config.Config
	web          *web.Handler
	mpesaClient  *mpesa.Client
	httpServer   *http.Server
	loginLimiter *LoginLimiter
	metrics      *Metrics
}

type LoginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
	max      int
	window   time.Duration
}

type loginAttempt struct {
	count int
	until time.Time
}

func NewLoginLimiter(max int, window time.Duration) *LoginLimiter {
	return &LoginLimiter{
		attempts: make(map[string]*loginAttempt),
		max:      max,
		window:   window,
	}
}

func (l *LoginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	a, exists := l.attempts[ip]
	if !exists || now.After(a.until) {
		l.attempts[ip] = &loginAttempt{count: 1, until: now.Add(l.window)}
		return true
	}
	a.count++
	return a.count <= l.max
}

func New(database *db.DB, cfg *config.Config) *Server {
	authSvc := auth.NewService(cfg.JWTSecret)

	eng, err := engine.NewEngineWithDB("server.db")
	if err != nil {
		slog.Warn("failed to initialize engine, using in-memory", "error", err)
		eng = engine.NewEngine()
	} else {
		slog.Info("engine initialized successfully")
	}

	var mpesaClient *mpesa.Client
	if cfg.Mpesa.ConsumerKey != "" && cfg.Mpesa.ConsumerSecret != "" {
		mpesaClient = mpesa.NewClient(mpesa.Config{
			ConsumerKey:    cfg.Mpesa.ConsumerKey,
			ConsumerSecret: cfg.Mpesa.ConsumerSecret,
			ShortCode:      cfg.Mpesa.ShortCode,
			Passkey:        cfg.Mpesa.Passkey,
			Environment:    cfg.Env,
			CallbackURL:    cfg.Mpesa.CallbackURL,
			APIVersion:     cfg.Mpesa.APIVersion,
		})
		slog.Info("M-Pesa client initialized", "environment", cfg.Env, "api_version", cfg.Mpesa.APIVersion)
	} else {
		slog.Warn("M-Pesa not configured — STK Push will be simulated")
	}

	webHandler := web.NewHandler(database, authSvc, mpesaClient, eng)

	s := &Server{
		router:       chi.NewRouter(),
		db:           database,
		engine:       eng,
		auth:         authSvc,
		cfg:          cfg,
		web:          webHandler,
		mpesaClient:  mpesaClient,
		loginLimiter: NewLoginLimiter(5, 5*time.Minute),
		metrics:      NewMetrics(),
	}

	s.mountMiddleware()
	s.mountRoutes()
	webHandler.RegisterRoutes(s.router)

	return s
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) mountMiddleware() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(30 * time.Second))
	s.router.Use(middleware.RequestSize(1 << 20)) // 1 MB body limit
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:8080"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	s.router.Use(RequestLogger)
}

func (s *Server) mountRoutes() {
	s.router.Get("/health", s.healthCheck)
	s.router.Get("/ready", s.readinessCheck)
	s.router.Get("/metrics", s.metricsHandler)
	s.router.Get("/static/*", func(w http.ResponseWriter, r *http.Request) {
		fs := http.FileServer(http.Dir("web/static"))
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/static")
		fs.ServeHTTP(w, r)
	})

	s.router.Post("/api/auth/login", s.login)
	s.router.Post("/api/auth/refresh", s.refreshToken)

	s.router.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(s.auth))
		r.Use(s.metricsMiddleware)

		r.Get("/api/dashboard", s.dashboard)

		r.Get("/api/products", s.listProducts)
		r.Post("/api/products", s.createProduct)
		r.Put("/api/products/{id}", s.updateProduct)
		r.Post("/api/products/{id}/stock", s.addStock)

		r.Post("/api/sales", s.recordSale)
		r.Get("/api/sales", s.listSales)
		r.Get("/api/sales/{id}/receipt", s.getReceipt)

		r.Post("/api/mpesa/pay", s.mpesaPay)
		r.Post("/api/mpesa/callback", s.mpesaCallback)

		r.Get("/api/workers", s.listWorkers)
		r.Post("/api/workers", s.createWorker)

		r.Post("/api/sessions", s.openSession)
		r.Post("/api/sessions/{id}/close", s.closeSession)
		r.Get("/api/sessions", s.listSessions)

		r.Post("/api/reconcile", s.reconcile)
		r.Get("/api/reconcile/{worker_id}", s.workerReconciliations)

		r.Get("/api/reports/daily", s.dailyReport)
		r.Get("/api/reports/worker", s.workerReport)
		r.Get("/api/reports/ledger", s.ledgerReport)
		r.Get("/api/reports/export", s.exportCSV)
		r.Post("/api/reports/email", s.sendReportEmail)

		r.Get("/api/ghost", s.ghostReport)

		r.Group(func(admin chi.Router) {
			admin.Use(RoleMiddleware("owner"))
			r.Get("/api/admin/users", s.listUsers)
			r.Post("/api/admin/users", s.createUser)
			r.Put("/api/admin/users/{id}/role", s.updateUserRole)
			r.Delete("/api/admin/users/{id}", s.disableUser)
		})
	})
}

func (s *Server) Start(addr string) error {
	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}
	return s.engine.Close()
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

func respondCreated(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(s.metrics.PrometheusOutput()))
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.metrics.IncActiveConns()
		defer s.metrics.DecActiveConns()

		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		duration := time.Since(start)

		routePattern := chi.RouteContext(r.Context()).RoutePattern()
		if routePattern == "" {
			routePattern = r.URL.Path
		}
		s.metrics.Record(r.Method, routePattern, ww.Status(), duration)
	})
}
