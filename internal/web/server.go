// Package web serves the REST API and the embedded single-page UI.
package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"recipearr/internal/pipeline"
	"recipearr/internal/store"
)

//go:embed all:ui
var uiFS embed.FS

type Server struct {
	st  *store.Store
	svc *pipeline.Service
}

func New(st *store.Store, svc *pipeline.Service) *Server {
	return &Server{st: st, svc: svc}
}

// Handler builds the HTTP routes (Go 1.22 method+pattern routing).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)

	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	mux.HandleFunc("POST /api/settings/test", s.handleTestConnection)

	mux.HandleFunc("GET /api/sources", s.handleListSources)
	mux.HandleFunc("POST /api/sources", s.handleCreateSource)
	mux.HandleFunc("PUT /api/sources/{id}", s.handleUpdateSource)
	mux.HandleFunc("DELETE /api/sources/{id}", s.handleDeleteSource)
	mux.HandleFunc("POST /api/sources/{id}/run", s.handleRunSource)

	mux.HandleFunc("GET /api/rules", s.handleListRules)
	mux.HandleFunc("POST /api/rules", s.handleCreateRule)
	mux.HandleFunc("PUT /api/rules/{id}", s.handleUpdateRule)
	mux.HandleFunc("DELETE /api/rules/{id}", s.handleDeleteRule)

	mux.HandleFunc("GET /api/items", s.handleListItems)
	mux.HandleFunc("POST /api/import", s.handleImport)

	// Static SPA from the embedded ui/ directory.
	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServerFS(sub))

	return mux
}

// ---- JSON helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
