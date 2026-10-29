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
)

// Server holds the HTTP state.
type Server struct {
	Cfg    config.Config
	Logger *slog.Logger
	OAuth  *auth.Server

	lic   *session.Licensing
	files *filestore.Store

	// Pre-auth mode: the shared session, opened at start-up or lazily on the
	// first request when the license (or the network) was not there yet.
	preMu   sync.Mutex
	preAuth *session.Session
	preErr  error
	preAt   time.Time

	mu      sync.Mutex
	servers map[*session.Session]*mcp.Server
	byCreds map[string]*session.Session // header-mode sessions keyed by credential hash
}

// New builds the server. preAuth and files may be nil; lic is the process-wide
// license manager (its absence makes every login fail).
func New(cfg config.Config, logger *slog.Logger, oauth *auth.Server, lic *session.Licensing, preAuth *session.Session, files *filestore.Store) *Server {
	return &Server{Cfg: cfg, Logger: logger, OAuth: oauth, lic: lic, preAuth: preAuth, files: files,
		servers: map[*session.Session]*mcp.Server{}, byCreds: map[string]*session.Session{}}
}

type ctxKey int

const sessionKey ctxKey = 1

// mcpServerFor returns (creating once) the MCP server bound to a session.
// This is the single place where a session meets an MCP server — the
// streamable endpoint and the REST bridge both come through here — so the
// license gate is installed here and nothing reaches a tool without it.
func (s *Server) mcpServerFor(sess *session.Session) *mcp.Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	if srv := s.servers[sess]; srv != nil {
		return srv
	}
	srv := tools.NewServer(sess, s.Cfg, s.Logger, s.files)
	srv.AddReceivingMiddleware(licenseGate(sess, s.Logger))
	s.servers[sess] = srv
	return srv
}

// licenseGate refuses every MCP request that touches Vchasno while the
// session's license is refused, unconfirmed or moved away. A refused tool call
// answers with a tool error carrying the reason (so the model can tell the
// user); resource reads and prompts answer with a protocol error. The
// handshake and the static lists stay open, and so does self_check, which
// reports the license status itself.
func licenseGate(sess *session.Session, logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if ungated(method) {
				return next(ctx, method, req)
			}
			var tool string
			if ctr, isCall := req.(*mcp.CallToolRequest); isCall && ctr.Params != nil {
				tool = ctr.Params.Name
				if tool == "self_check" {
					return next(ctx, method, req)
				}
			}
			if err := sess.CheckLicense(ctx); err != nil {
				logger.Warn("request refused by the license gate", "method", method, "tool", tool, "err", err)
				if method == "tools/call" {
					return licenseToolError(err), nil
				}
				return nil, err
			}
			return next(ctx, method, req)
		}
	}
}

func ungated(method string) bool {
	switch method {
	case "initialize", "ping", "tools/list", "resources/list", "resources/templates/list", "prompts/list", "logging/setLevel":
		return true
	}
	return strings.HasPrefix(method, "notifications/")
}

func licenseToolError(err error) *mcp.CallToolResult {
	body := map[string]any{"error": err.Error(), "hint": "the license does not allow this server to work right now; run self_check for the license status"}
	if le, isLic := session.AsLicenseError(err); isLic {
		body["code"] = le.Code
		if le.Message != "" {
			body["message"] = le.Message
		}
	}
	data, _ := json.MarshalIndent(body, "", "  ")
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
}

// open is the one way this server opens a session: licensed, through the
// OAuth server's connector (session.Open).
func (s *Server) open(ctx context.Context, login session.Login) (*session.Session, error) {
	return s.OAuth.Connect(ctx, login)
}

// preAuthRetry is how often a failed pre-auth connection is retried on demand.
const preAuthRetry = 30 * time.Second

// preAuthSession returns the shared pre-auth session, opening it when it is not
// open yet (licence or network were unavailable at start-up).
func (s *Server) preAuthSession(ctx context.Context) (*session.Session, error) {
	s.preMu.Lock()
	defer s.preMu.Unlock()
	if s.preAuth != nil {
		return s.preAuth, nil
	}
	if s.preErr != nil && time.Since(s.preAt) < preAuthRetry {
		return nil, s.preErr
	}
	sess, err := s.open(ctx, session.Login{LicenseKey: s.Cfg.LicenseKey, Connection: s.Cfg.LicenseConnection, Token: s.Cfg.Token})
	s.preAt = time.Now()
	if err != nil {
		s.preErr = err
		return nil, err
	}
	s.preAuth, s.preErr = sess, nil
	return sess, nil
}

// PreAuthFailed records why the start-up pre-auth connection failed, so that
// requests answer with that reason until a retry succeeds.
func (s *Server) PreAuthFailed(err error) {
	s.preMu.Lock()
	s.preErr, s.preAt = err, time.Now()
	s.preMu.Unlock()
}

func (s *Server) currentPreAuth() *session.Session {
	s.preMu.Lock()
	defer s.preMu.Unlock()
	return s.preAuth
}

// resolveSession applies the three auth modes in order. Every mode opens its
// session through session.Open, so a session exists only for a confirmed
// license; later requests are gated per call by licenseGate.
func (s *Server) resolveSession(r *http.Request) (*session.Session, error) {
	if s.Cfg.PreAuth() {
		return s.preAuthSession(r.Context())
	}
	// Header mode: the license key and the Vchasno token travel in headers of
	// their own; the connection header picks one of several connections.
	if tok := strings.TrimSpace(r.Header.Get(s.Cfg.HeaderToken)); tok != "" {
		key := strings.TrimSpace(r.Header.Get(s.Cfg.HeaderLicenseKey))
		conn := strings.TrimSpace(r.Header.Get(s.Cfg.HeaderConnection))
		if key == "" {
			if err := s.lic.Err(); err != nil {
				return nil, err
			}
			return nil, errUnauthorized{desc: "the " + s.Cfg.HeaderLicenseKey + " header is required together with " + s.Cfg.HeaderToken}
		}
		sum := sha256.Sum256([]byte(key + "\x00" + conn + "\x00" + tok))
		ck := hex.EncodeToString(sum[:])
		s.mu.Lock()
		sess := s.byCreds[ck]
		s.mu.Unlock()
		if sess != nil {
			return sess, nil
		}
		sess, err := s.open(r.Context(), session.Login{LicenseKey: key, Connection: conn, Token: tok})
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		if existing := s.byCreds[ck]; existing != nil {
			s.mu.Unlock()
			sess.Close()
			return existing, nil
		}
		s.byCreds[ck] = sess
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
		if _, isLic := session.AsLicenseError(err); isLic {
			return nil, err
		}
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
			if _, isLic := session.AsLicenseError(err); isLic {
				http.Error(w, "license check failed: "+err.Error(), http.StatusForbidden)
				return
			}
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
		// Ready means "can serve MCP": licensing must be configured, and in
		// pre-auth mode the license must hold and the company's API must be
		// open, otherwise every tool would answer with an error. /health stays
		// green regardless, so that the container is not restarted in a loop.
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{"ok": true, "license_configured": s.lic.Err() == nil}
		fail := func(reason string) {
			body["ok"] = false
			body["reason"] = reason
		}
		if err := s.lic.Err(); err != nil {
			fail(err.Error())
		} else if s.Cfg.PreAuth() {
			sess, err := s.preAuthSession(r.Context())
			switch {
			case err != nil:
				fail(err.Error())
			default:
				if lerr := sess.CheckLicense(r.Context()); lerr != nil {
					fail(lerr.Error())
				} else if !sess.APIOpen() {
					fail(sess.APINote())
				}
				body["api_open"] = sess.APIOpen()
				st := sess.LicenseStatus()
				body["license"] = map[string]any{"ok": st.OK, "license_id": st.LicenseID, "connection": st.Connection,
					"valid_until": st.ValidUntil, "warnings": st.Warnings}
			}
		}
		if body["ok"] != true {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "vchasno-edo-mcp", "version": tools.Version, "mcp": s.Cfg.IssuerURL + "/mcp", "openapi": s.Cfg.IssuerURL + "/mcp/openapi.json",
			"read_only": s.Cfg.ReadOnly,
			"license":   map[string]any{"configured": s.lic.Err() == nil, "product": config.LicenseProduct, "instance_id": s.lic.InstanceID()},
			"auth": map[string]any{"pre_auth": s.Cfg.PreAuth(), "headers": []string{s.Cfg.HeaderLicenseKey, s.Cfg.HeaderToken, s.Cfg.HeaderConnection},
				"oauth": s.Cfg.IssuerURL + "/.well-known/oauth-authorization-server"}})
	})
	s.OAuth.Routes(mux)

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
