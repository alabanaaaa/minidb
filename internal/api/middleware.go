package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"mini-database/internal/auth"
)

type contextKey string

const (
	ContextUserID    contextKey = "user_id"
	ContextShopID    contextKey = "shop_id"
	ContextUserEmail contextKey = "user_email"
	ContextUserRole  contextKey = "user_role"
)

func AuthMiddleware(authSvc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				respondError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			tokenString := strings.TrimPrefix(header, "Bearer ")
			if tokenString == header {
				respondError(w, http.StatusUnauthorized, "invalid authorization format, use: Bearer <token>")
				return
			}

			claims, err := authSvc.VerifyToken(tokenString)
			if err != nil {
				respondError(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), ContextUserID, claims.UserID)
			ctx = context.WithValue(ctx, ContextShopID, claims.ShopID)
			ctx = context.WithValue(ctx, ContextUserEmail, claims.Email)
			ctx = context.WithValue(ctx, ContextUserRole, claims.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RoleMiddleware(requiredRole string) func(http.Handler) http.Handler {
	roleHierarchy := map[string]int{
		"cashier": 1,
		"manager": 2,
		"owner":   3,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userRole, ok := r.Context().Value(ContextUserRole).(string)
			if !ok {
				respondError(w, http.StatusUnauthorized, "no user context")
				return
			}

			userLevel := roleHierarchy[userRole]
			requiredLevel := roleHierarchy[requiredRole]

			if userLevel < requiredLevel {
				respondError(w, http.StatusForbidden, fmt.Sprintf("requires %s role", requiredRole))
				return
			}

			next.ServeHTTP(w, r.WithContext(r.Context()))
		})
	}
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		start := time.Now()

		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(ww, r)

		slog.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.statusCode,
			"duration", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func GetShopID(r *http.Request) string {
	v, _ := r.Context().Value(ContextShopID).(string)
	return v
}

func GetUserID(r *http.Request) string {
	v, _ := r.Context().Value(ContextUserID).(string)
	return v
}

func GetUserRole(r *http.Request) string {
	v, _ := r.Context().Value(ContextUserRole).(string)
	return v
}

func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
