package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mini-database/internal/auth"
	"mini-database/internal/config"
	"mini-database/internal/db"
	"mini-database/internal/web"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Server struct {
	router *chi.Mux
	db     *db.DB
	auth   *auth.Service
	cfg    *config.Config
	web    *web.Handler
}

func New(database *db.DB, cfg *config.Config) *Server {
	authSvc := auth.NewService(cfg.JWTSecret)
	webHandler := web.NewHandler(database, authSvc)

	s := &Server{
		router: chi.NewRouter(),
		db:     database,
		auth:   authSvc,
		cfg:    cfg,
		web:    webHandler,
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
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
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

	s.router.Post("/api/auth/login", s.login)
	s.router.Post("/api/auth/refresh", s.refreshToken)

	s.router.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(s.auth))

		r.Get("/api/dashboard", s.dashboard)

		r.Get("/api/products", s.listProducts)
		r.Post("/api/products", s.createProduct)
		r.Put("/api/products/{id}", s.updateProduct)
		r.Post("/api/products/{id}/stock", s.addStock)

		r.Post("/api/sales", s.recordSale)
		r.Get("/api/sales", s.listSales)
		r.Get("/api/sales/{id}/receipt", s.getReceipt)

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
	server := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	server := &http.Server{
		Addr:    ":" + fmt.Sprint(s.cfg.HTTPPort),
		Handler: s.Handler(),
	}
	return server.Shutdown(ctx)
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
