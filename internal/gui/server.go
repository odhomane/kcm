// Package gui provides the embedded web dashboard.
package gui

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/odhomane/kcm/internal/core"
	"github.com/odhomane/kcm/internal/ops"
	"github.com/odhomane/kcm/internal/store"
)

//go:embed static
var staticFS embed.FS

// Server is the HTTP dashboard server.
type Server struct {
	mgr    *core.Manager
	st     *store.Store
	op     *ops.Ops
	host   string
	port   int
	token  string
	mux    *http.ServeMux
	events chan string // SSE broadcast
}

// NewServer creates a Server. Call Run() to start listening.
func NewServer(mgr *core.Manager, st *store.Store, op *ops.Ops, host string, port int) *Server {
	s := &Server{
		mgr:    mgr,
		st:     st,
		op:     op,
		host:   host,
		port:   port,
		events: make(chan string, 32),
	}
	s.token = mustToken()
	s.registerRoutes()
	return s
}

// Run starts the HTTP server and (optionally) opens the browser.
func (s *Server) Run(noBrowser bool) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}

	url := fmt.Sprintf("http://%s/?token=%s", addr, s.token)
	fmt.Printf("kcm dashboard: %s\n", url)
	fmt.Printf("One-time token: %s\n", s.token)

	if s.host != "127.0.0.1" {
		fmt.Fprintf(os.Stderr,
			"WARNING: bound to %s — dashboard is accessible on the network!\n", s.host)
	}

	// Watch kubeconfig files for changes.
	go s.watchFiles()

	if !noBrowser {
		go openBrowser(url)
	}

	srv := &http.Server{
		Handler:      s.mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return srv.Serve(ln)
}

// registerRoutes sets up all HTTP handlers.
func (s *Server) registerRoutes() {
	mux := http.NewServeMux()

	// Auth middleware wraps protected handlers.
	auth := s.authMiddleware

	// Static assets (all embedded, no CDN).
	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

	// Dashboard UI.
	mux.Handle("/", auth(http.HandlerFunc(s.handleIndex)))

	// API endpoints (HTMX targets).
	mux.Handle("/api/contexts", auth(http.HandlerFunc(s.handleContexts)))
	mux.Handle("/api/contexts/use", auth(http.HandlerFunc(s.handleUse)))
	mux.Handle("/api/contexts/rename", auth(http.HandlerFunc(s.handleRename)))
	mux.Handle("/api/contexts/delete", auth(http.HandlerFunc(s.handleDelete)))
	mux.Handle("/api/contexts/export", auth(http.HandlerFunc(s.handleExport)))
	mux.Handle("/api/contexts/group", auth(http.HandlerFunc(s.handleSetGroup)))
	mux.Handle("/api/contexts/label", auth(http.HandlerFunc(s.handleSetLabel)))
	mux.Handle("/api/contexts/color", auth(http.HandlerFunc(s.handleSetColor)))
	mux.Handle("/api/contexts/pin", auth(http.HandlerFunc(s.handlePin)))
	mux.Handle("/api/namespaces", auth(http.HandlerFunc(s.handleNamespaces)))
	mux.Handle("/api/health", auth(http.HandlerFunc(s.handleHealth)))
	mux.Handle("/api/groups", auth(http.HandlerFunc(s.handleGroups)))
	mux.Handle("/api/files", auth(http.HandlerFunc(s.handleFiles)))
	mux.Handle("/api/validate", auth(http.HandlerFunc(s.handleValidate)))

	// Server-Sent Events for live updates.
	mux.Handle("/events", auth(http.HandlerFunc(s.handleSSE)))

	s.mux = mux
}

// authMiddleware checks the token cookie or Authorization header.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow token via query param (for initial browser open).
		if t := r.URL.Query().Get("token"); t == s.token {
			// Set cookie and redirect without token in URL.
			http.SetCookie(w, &http.Cookie{
				Name:     "kcm_token",
				Value:    s.token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, r.URL.Path, http.StatusFound)
			return
		}
		// Check cookie.
		if c, err := r.Cookie("kcm_token"); err == nil && c.Value == s.token {
			next.ServeHTTP(w, r)
			return
		}
		// Check Authorization header.
		if r.Header.Get("Authorization") == "Bearer "+s.token {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// watchFiles watches kubeconfig files for changes and broadcasts SSE events.
func (s *Server) watchFiles() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer w.Close()
	for _, p := range s.mgr.Paths() {
		_ = w.Add(p)
	}
	for {
		select {
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				_ = s.mgr.Load(event.Name)
				select {
				case s.events <- "kubeconfig-changed":
				default:
				}
			}
		case <-w.Errors:
		}
	}
}

// handleSSE streams Server-Sent Events to the client.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Heartbeat every 15s to keep connection alive.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.events:
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func mustToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("generating token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func openBrowser(url string) {
	time.Sleep(500 * time.Millisecond)
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "linux":
		cmd, args = "xdg-open", []string{url}
	default:
		return
	}
	_ = exec.Command(cmd, args...).Start()
}
