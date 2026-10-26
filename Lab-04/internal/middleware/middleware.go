package middleware

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"deviceregistry/internal/domain"
)

// responseWriterWrapper перехоплює статус-код для логування
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// LoggerMiddleware здійснює структуроване логування кожного HTTP-запиту (Рівень 2)
func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapper := &responseWriterWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // За замовчуванням 200
		}

		next.ServeHTTP(wrapper, r)

		duration := time.Since(start)

		clientIP := r.RemoteAddr
		if colon := strings.LastIndex(clientIP, ":"); colon != -1 {
			clientIP = clientIP[:colon]
		}
		if clientIP == "[" || clientIP == "" {
			clientIP = "127.0.0.1"
		}

		statusText := http.StatusText(wrapper.statusCode)
		path := r.URL.Path
		if r.URL.RawQuery != "" {
			path += "?" + r.URL.RawQuery
		}

		fmt.Printf("[HTTP] %s - %s %s -> %d %s (%v)\n",
			clientIP, r.Method, path, wrapper.statusCode, statusText, duration.Round(time.Microsecond))
	})
}

// RecovererMiddleware перехоплює аварійні збої (Panic) та повертає 500 JSON (Рівень 3)
func RecovererMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[PANIC RECOVER] %v\nStack trace:\n%s", rec, debug.Stack())

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)

				errResp := domain.ErrorResponse{
					Error:     "Internal Server Error",
					Message:   fmt.Sprintf("Непередбачена внутрішня помилка сервера: %v", rec),
					Status:    http.StatusInternalServerError,
					Timestamp: time.Now().UTC(),
				}

				_ = json.NewEncoder(w).Encode(errResp)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware налаштовує заголовки міждоменного доступу (Рівень 3)
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
