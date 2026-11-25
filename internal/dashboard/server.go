package dashboard

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ServerConfig holds the configuration for the dashboard HTTP server.
type ServerConfig struct {
	Host    string
	Port    int
	DataDir string
	WebDir  string
}

// Server encapsulates the ForkRisk web dashboard HTTP server.
type Server struct {
	cfg        ServerConfig
	httpServer *http.Server
	loader     *DataLoader
	handler    *Handler
}

// NewServer initializes a new dashboard server.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "outputs"
	}
	if cfg.WebDir == "" {
		cfg.WebDir = "web"
	}

	loader := NewDataLoader(cfg.DataDir)
	h := NewHandler(loader)

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/health", h.HandleHealth)
	mux.HandleFunc("/api/runs", h.HandleRuns)
	mux.HandleFunc("/api/runs/", h.HandleRunDetail)
	mux.HandleFunc("/api/scenarios/summary", h.HandleScenariosSummary)
	mux.HandleFunc("/api/models/evaluation", h.HandleModelEvaluation)
	mux.HandleFunc("/api/drift", h.HandleDrift)
	mux.HandleFunc("/api/sensitivity", h.HandleSensitivity)

	// Static web assets serving
	cleanWebDir := filepath.Clean(cfg.WebDir)
	fileServer := http.FileServer(http.Dir(cleanWebDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Strict path traversal prevention for static files
		if strings.Contains(r.URL.Path, "..") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		// If requesting root or path doesn't have an extension, try index.html
		requestedFile := filepath.Join(cleanWebDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(requestedFile); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		indexPath := filepath.Join(cleanWebDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &Server{
		cfg:        cfg,
		httpServer: srv,
		loader:     loader,
		handler:    h,
	}
}

// Start begins listening and serving HTTP requests.
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.Addr())
	if err != nil {
		return err
	}
	return s.Serve(listener)
}

// Serve accepts dashboard requests on an already bound listener.
func (s *Server) Serve(listener net.Listener) error {
	log.Printf("[Dashboard] Server listening at http://%s\n", listener.Addr())
	log.Printf("[Dashboard] Serving static assets from: %s\n", s.cfg.WebDir)
	log.Printf("[Dashboard] Data directory: %s\n", s.cfg.DataDir)
	return s.httpServer.Serve(listener)
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the configured address.
func (s *Server) Addr() string {
	return s.httpServer.Addr
}
