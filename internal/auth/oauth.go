package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/session"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// Connector opens a Vchasno session for validated credentials.
type Connector func(ctx context.Context, creds vchasno.Credentials) (*session.Session, error)

// RegisteredClient is a dynamically registered OAuth client (RFC 7591).
type RegisteredClient struct {
	ClientID     string
	ClientName   string
	RedirectURIs []string
	CreatedAt    time.Time
}

type authCode struct {
	clientID            string
	redirectURI         string
	codeChallenge       string
	codeChallengeMethod string
	session             *session.Session
	createdAt           time.Time
}

// Server is the in-memory OAuth 2.1 authorization server.
type Server struct {
	Issuer      string
	ResourceURL string
	Tokens      *TokenManager
	Connect     Connector
	Logger      *slog.Logger

	mu       sync.Mutex
	clients  map[string]*RegisteredClient
	codes    map[string]*authCode
	sessions map[string]*session.Session // token → live session
}

// NewServer creates the OAuth server.
func NewServer(issuer, resourceURL string, tokens *TokenManager, connect Connector, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{Issuer: issuer, ResourceURL: resourceURL, Tokens: tokens, Connect: connect, Logger: logger,
		clients: map[string]*RegisteredClient{}, codes: map[string]*authCode{}, sessions: map[string]*session.Session{}}
}

// RegisterClient stores a new client and returns its id.
func (s *Server) RegisterClient(name string, redirectURIs []string) *RegisteredClient {
	c := &RegisteredClient{ClientID: randomToken(16), ClientName: name, RedirectURIs: redirectURIs, CreatedAt: time.Now()}
	s.mu.Lock()
	s.clients[c.ClientID] = c
	s.mu.Unlock()
	s.Logger.Info("oauth client registered", "client_id", c.ClientID, "name", name)
	return c
}

// IsValidClient checks that the client exists and the redirect URI was registered.
func (s *Server) IsValidClient(clientID, redirectURI string) bool {
	s.mu.Lock()
	c := s.clients[clientID]
	s.mu.Unlock()
	if c == nil {
		return false
	}
	for _, u := range c.RedirectURIs {
		if u == redirectURI {
			return true
		}
	}
	return false
}

// CreateAuthCode issues a single-use authorization code bound to a live session.
func (s *Server) CreateAuthCode(clientID, redirectURI, challenge, method string, sess *session.Session) string {
	code := randomToken(32)
	s.mu.Lock()
	s.codes[code] = &authCode{clientID: clientID, redirectURI: redirectURI, codeChallenge: challenge, codeChallengeMethod: method, session: sess, createdAt: time.Now()}
	// Drop stale codes.
	for k, v := range s.codes {
		if time.Since(v.createdAt) > 10*time.Minute {
			delete(s.codes, k)
		}
	}
	s.mu.Unlock()
	return code
}

// ExchangeCode validates the code + PKCE verifier and returns an access token.
func (s *Server) ExchangeCode(code, clientID, redirectURI, verifier string) (string, error) {
	s.mu.Lock()
	ac := s.codes[code]
	delete(s.codes, code)
	s.mu.Unlock()
	if ac == nil {
		return "", fmt.Errorf("unknown authorization code")
	}
	if time.Since(ac.createdAt) > 5*time.Minute {
		return "", fmt.Errorf("authorization code expired")
	}
	if ac.clientID != clientID || ac.redirectURI != redirectURI {
		return "", fmt.Errorf("client_id or redirect_uri mismatch")
	}
	if !verifyPKCE(verifier, ac.codeChallenge, ac.codeChallengeMethod) {
		return "", fmt.Errorf("PKCE verification failed")
	}
	token, err := s.Tokens.CreateToken(ac.session.Creds.BaseURL, ac.session.Creds.Token)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[token] = ac.session
	s.mu.Unlock()
	s.Logger.Info("access token issued", "client_id", clientID, "token", ac.session.Creds.Redacted(), "base", ac.session.Creds.BaseURL)
	return token, nil
}

// SessionByToken resolves a Bearer token to a live session, re-creating the
// session from the token's encrypted Vchasno credentials after a restart.
func (s *Server) SessionByToken(ctx context.Context, token string) (*session.Session, error) {
	s.mu.Lock()
	if sess := s.sessions[token]; sess != nil {
		s.mu.Unlock()
		return sess, nil
	}
	s.mu.Unlock()

	creds, err := s.Tokens.Validate(token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	sess, err := s.Connect(ctx, vchasno.Credentials{BaseURL: creds.BaseURL, Token: creds.Token})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if existing := s.sessions[token]; existing != nil {
		s.mu.Unlock()
		sess.Close()
		return existing, nil
	}
	s.sessions[token] = sess
	s.mu.Unlock()
	s.Logger.Info("session restored from token", "sub", creds.Subject, "base", creds.BaseURL)
	return sess, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func verifyPKCE(verifier, challenge, method string) bool {
	if method != "S256" && method != "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}
