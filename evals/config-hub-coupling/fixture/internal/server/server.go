package server

import (
	"net/http"
	"time"

	"example.com/shop/internal/db"
)

// Server is the application's HTTP server.
type Server struct {
	http *http.Server
	pool *db.Pool
}

// New builds the HTTP server.
func New(pool *db.Pool) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return &Server{
		http: &http.Server{
			Addr:         "localhost:8080",
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
		pool: pool,
	}
}

// ListenAndServe starts the server.
func (s *Server) ListenAndServe() error { return s.http.ListenAndServe() }
