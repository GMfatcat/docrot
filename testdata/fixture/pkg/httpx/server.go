// Package httpx is a tiny HTTP helper used by the fixture docs.
package httpx

import "net/http"

// Config is the server configuration.
type Config struct {
	Server struct {
		Addr    string `json:"addr"`
		Timeout int    `json:"timeout_ms" default:"5000"`
	} `json:"server"`
	Log LogConfig `json:"log"`
}

// LogConfig controls logging.
type LogConfig struct {
	Level  string `json:"level" default:"info"`
	Format string `json:"format"`
}

// Server wraps http.Server.
type Server struct {
	addr string
}

// NewServer creates a Server listening on addr. The tlsConfig argument
// was removed in v2; see docs/guide.md for migration notes.
func NewServer(addr string) *Server { return &Server{addr: addr} }

// Addr returns the listen address.
func (s *Server) Addr() string { return s.addr }

// WriteData writes a JSON envelope.
func WriteData(w http.ResponseWriter, v any) error { return nil }

// WriteError writes an error envelope.
func WriteError(w http.ResponseWriter, err error) {}

// Undocumented is never mentioned in any doc.
func Undocumented() {}

// Routes registers the HTTP API on mux.
func Routes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /v1/items", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("POST /v1/items", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("GET /v1/items/{id}", func(w http.ResponseWriter, r *http.Request) {})
}
