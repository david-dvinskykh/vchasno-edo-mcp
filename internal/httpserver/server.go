// Package httpserver wires HTTP routing: health, OAuth endpoints, the MCP
// Streamable HTTP endpoint with three authentication modes (pre-auth env,
// credential header, OAuth Bearer), an OpenAPI description and a REST bridge
// so that non-MCP clients (Open WebUI, curl) can call the same tools.
package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/auth"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/config"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/filestore"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/session"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/tools"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// Server holds the HTTP state.
type Server struct {
	Cfg    config.Config
	Logger *slog.Logger
	OAuth  *auth.Server

	preAuth *session.Session
	files   *filestore.Store

	mu      sync.Mutex
	servers map[*session.Session]*mcp.Server
	byCreds map[string]*session.Session // header-mode sessions keyed by credential hash
}

// New builds the server. preAuth and files may be nil.
func New(cfg config.Config, logger *slog.Logger, oauth *auth.Server, preAuth *session.Session, files *filestore.Store) *Server {
	return &Server{Cfg: cfg, Logger: logger, OAuth: oauth, preAuth: preAuth, files: files,
		servers: map[*session.Session]*mcp.Server{}, byCreds: map[string]*session.Session{}}
}

type ctxKey int

const sessionKey ctxKey = 1

// mcpServerFor returns (creating once) the MCP server bound to a session.
func (s *Server) mcpServerFor(sess *session.Session) *mcp.Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	if srv := s.servers[sess]; srv != nil {
		return srv
	}
	srv := tools.NewServer(sess, s.Cfg, s.Logger, s.files)
	s.servers[sess] = srv
	return srv
}

// resolveSession applies the three auth modes in order.
func (s *Server) resolveSession(r *http.Request) (*session.Session, error) {
	if s.preAuth != nil {
		return s.preAuth, nil
	}
	// Header mode: the Vchasno token travels in a header of its own, and the
	// host may be overridden alongside it.
	if tok := strings.TrimSpace(r.Header.Get(s.Cfg.HeaderToken)); tok != "" {
		host := strings.TrimSpace(r.Header.Get(s.Cfg.HeaderURL))
		if host == "" {
			host = s.Cfg.BaseURL
		}
		sum := sha256.Sum256([]byte(host + "\x00" + tok))
		key := hex.EncodeToString(sum[:])
		s.mu.Lock()
		sess := s.byCreds[key]
		s.mu.Unlock()
		if sess != nil {
			return sess, nil
		}
		sess, err := session.Connect(r.Context(), s.Cfg, vchasno.Credentials{BaseURL: host, Token: tok}, s.Logger)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		if existing := s.byCreds[key]; existing != nil {
			s.mu.Unlock()
			sess.Close()
			return existing, nil
		}
		s.byCreds[key] = sess
		s.mu.Unlock()
		return sess, nil
	}
	// OAuth Bearer.
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return nil, errUnauthorized{desc: "Missing Authorization header"}
	}
	token := strings.TrimSpace(authz[7:])
	sess, err := s.OAuth.SessionByToken(r.Context(), token)
	if err != nil {
		return nil, errUnauthorized{desc: "Invalid or expired access token: " + err.Error()}
	}
	return sess, nil
}

type errUnauthorized struct{ desc string }

func (e errUnauthorized) Error() string { return e.desc }

func (s *Server) writeUnauthorized(w http.ResponseWriter, desc string) {
	meta := s.Cfg.IssuerURL + "/.well-known/oauth-protected-resource/mcp"
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer error="invalid_token", error_description=%q, resource_metadata=%q`, desc, meta))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_token", "error_description": desc})
}

// authMiddleware resolves the session and stores it in the request context.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.resolveSession(r)
		if err != nil {
			if ue, okc := err.(errUnauthorized); okc {
				s.Logger.Info("unauthorized MCP request", "path", r.URL.Path, "reason", ue.desc)
				s.writeUnauthorized(w, ue.desc)
				return
			}
			s.Logger.Warn("authentication failed", "err", err)
			http.Error(w, "authentication failed: "+err.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

func sessionFrom(r *http.Request) *session.Session {
	sess, _ := r.Context().Value(sessionKey).(*session.Session)
	return sess
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		// Ready means "can serve MCP": in pre-auth mode that also requires the
		// company's API to be open, otherwise every tool would answer 403.
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"ok": true}
		if s.preAuth != nil {
			body["api_open"] = s.preAuth.APIOpen
			if !s.preAuth.APIOpen {
				body["ok"] = false
				body["reason"] = s.preAuth.APINote
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "vchasno-edo-mcp", "version": tools.Version, "mcp": s.Cfg.IssuerURL + "/mcp", "openapi": s.Cfg.IssuerURL + "/mcp/openapi.json",
			"read_only": s.Cfg.ReadOnly,
			"auth":      map[string]any{"pre_auth": s.preAuth != nil, "headers": []string{s.Cfg.HeaderURL, s.Cfg.HeaderToken}, "oauth": s.Cfg.IssuerURL + "/.well-known/oauth-authorization-server"}})
	})
	s.OAuth.Routes(mux, s.Cfg.BaseURL)

	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		sess := sessionFrom(r)
		if sess == nil {
			return nil
		}
		return s.mcpServerFor(sess)
	}, &mcp.StreamableHTTPOptions{Logger: s.Logger, SessionTimeout: 6 * time.Hour, DisableLocalhostProtection: true})

	mux.Handle("/mcp", s.authMiddleware(mcpHandler))
	mux.Handle("/mcp/{$}", s.authMiddleware(mcpHandler))
	// Download links are deliberately outside the auth middleware: the id is
	// 256 bits of randomness and the entry expires, so the URL itself is the
	// capability. That is what lets a client fetch a signed ZIP directly
	// instead of dragging it through the conversation.
	mux.HandleFunc("GET /files/{id}", s.handleFile)
	mux.HandleFunc("HEAD /files/{id}", s.handleFile)
	mux.HandleFunc("GET /mcp/openapi.json", s.handleOpenAPI)
	mux.Handle("POST /tools/{name}", s.authMiddleware(http.HandlerFunc(s.handleToolCall)))
	mux.Handle("GET /tools", s.authMiddleware(http.HandlerFunc(s.handleToolList)))
	mux.Handle("GET /resources", s.authMiddleware(http.HandlerFunc(s.handleResourceList)))
	mux.Handle("GET /resources/read", s.authMiddleware(http.HandlerFunc(s.handleResourceRead)))
	mux.Handle("GET /prompts", s.authMiddleware(http.HandlerFunc(s.handlePromptList)))
	mux.Handle("POST /prompts/{name}", s.authMiddleware(http.HandlerFunc(s.handlePromptGet)))

	return s.logging(mux)
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		if r.URL.Path == "/health" {
			return
		}
		s.Logger.Info("http", "method", r.Method, "path", r.URL.Path, "status", rw.status, "dur", time.Since(start).Round(time.Millisecond), "session", r.Header.Get("Mcp-Session-Id"))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, okc := w.ResponseWriter.(http.Flusher); okc {
		f.Flush()
	}
}

// handleFile serves a downloaded document by the opaque id of its store entry.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	if s.files == nil {
		http.NotFound(w, r)
		return
	}
	entry, data, ok := s.files.Open(r.PathValue("id"))
	if !ok {
		// An expired link and a wrong one answer the same way on purpose.
		http.Error(w, "no such file, or the link has expired", http.StatusNotFound)
		return
	}
	ct := entry.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(entry.Filename)))
	w.Header().Set("X-Checksum-SHA256", entry.SHA256)
	w.Header().Set("Cache-Control", "private, no-store")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}
