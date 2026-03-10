package server

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/shamith/mongodiff/pkg/history"
	"github.com/shamith/mongodiff/pkg/profile"
	"github.com/shamith/mongodiff/web"
)

// Server is the HTTP server for mongodiff.
type Server struct {
	port        int
	mux         *http.ServeMux
	profilePath string
	historyDir  string
}

// New creates a new Server.
func New(port int) *Server {
	s := &Server{port: port, mux: http.NewServeMux(), profilePath: profile.DefaultPath(), historyDir: history.DefaultDir()}
	s.setupRoutes()
	return s
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	fmt.Printf("mongodiff server running at http://localhost%s\n", addr)
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) setupRoutes() {
	// API routes
	s.mux.HandleFunc("POST /api/diff", s.handleDiff)
	s.mux.HandleFunc("POST /api/diff/stream", s.handleDiffStream)
	s.mux.HandleFunc("POST /api/sync", s.handleSync)
	s.mux.HandleFunc("POST /api/sync/dry-run", s.handleSyncDryRun)
	s.mux.HandleFunc("POST /api/test-connection", s.handleTestConnection)
	s.mux.HandleFunc("POST /api/collections", s.handleListCollections)
	s.mux.HandleFunc("GET /api/profiles", s.handleGetProfiles)
	s.mux.HandleFunc("POST /api/profiles", s.handleSaveProfile)
	s.mux.HandleFunc("DELETE /api/profiles/{name}", s.handleDeleteProfile)
	s.mux.HandleFunc("POST /api/history", s.handleGetHistory)
	s.mux.HandleFunc("POST /api/history/export", s.handleExportHistory)

	// Serve embedded static assets
	distFS, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		panic(fmt.Sprintf("failed to create sub filesystem: %v", err))
	}
	fileServer := http.FileServer(http.FS(distFS))

	// SPA fallback: serve index.html for all non-API, non-file routes
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists
		f, err := distFS.Open(path[1:]) // strip leading /
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback — serve index.html
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
