package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/config"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/license"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

// ErrLicenseNotConfigured is what every login and every tool call answers when
// the license server is not configured. The process still runs (health stays
// green, so an auto-updater does not crash-loop it), it just refuses to work.
var ErrLicenseNotConfigured = errors.New("license is not configured (MCP_LICENSE_SERVER_URL / MCP_LICENSE_PUBLIC_KEY)")

// License error codes that do not come from the license server itself. A
// refusal by the server carries the server's reason instead (UNPAID,
// LICENSE_SUSPENDED, INSTANCE_LIMIT, ...).
const (
	LicenseNotConfigured    = "NOT_CONFIGURED"
	LicenseNoKey            = "NO_KEY"
	LicenseInvalidKey       = "INVALID_KEY"
	LicenseOffline          = "OFFLINE"
	LicenseConnection       = "CONNECTION"
	LicenseChooseConnection = "CHOOSE_CONNECTION"
)

// LicenseError is a refusal that comes from licensing rather than from Vchasno.
type LicenseError struct {
	// Code is one of the License* constants or the server's refusal reason.
	Code string
	// Message is the license server's own message, when it sent one.
	Message string
	// Connections lists the choices when Code is LicenseChooseConnection.
	Connections []license.Connection
	Err         error
}

func (e *LicenseError) Error() string { return e.Err.Error() }
func (e *LicenseError) Unwrap() error { return e.Err }

// AsLicenseError returns the license error inside err, if any.
func AsLicenseError(err error) (*LicenseError, bool) {
	var le *LicenseError
	if errors.As(err, &le) {
		return le, true
	}
	return nil, false
}

// Licensing is the one license manager of the process.
type Licensing struct {
	m      *license.Manager
	err    error // why m is nil
	logger *slog.Logger
}

// NewLicensing builds the process-wide license manager from the configuration.
// It never fails: a missing or broken configuration is logged and remembered,
// and every later Entitlement call answers with it.
func NewLicensing(cfg config.Config, appVersion string, logger *slog.Logger) *Licensing {
	if logger == nil {
		logger = slog.Default()
	}
	l := &Licensing{logger: logger}
	if !cfg.LicenseConfigured() {
		l.err = ErrLicenseNotConfigured
		logger.Error("licensing is not configured: every login and tool call will be refused", "err", l.err)
		return l
	}
	m, err := license.NewManager(license.Config{
		ServerURL:  cfg.LicenseServerURL,
		PublicKey:  cfg.LicensePublicKey,
		Product:    config.LicenseProduct,
		AppVersion: appVersion,
		StateDir:   cfg.JWTKeyDir,
	})
	if err != nil {
		l.err = fmt.Errorf("%w: %v", ErrLicenseNotConfigured, err)
		logger.Error("licensing is misconfigured: every login and tool call will be refused", "err", err)
		return l
	}
	l.m = m
	logger.Info("licensing enabled", "server", cfg.LicenseServerURL, "product", config.LicenseProduct, "instance", m.InstanceID())
	return l
}

// NewLicensingFromManager wraps an existing manager (tests use it to control
// the clock and the HTTP client).
func NewLicensingFromManager(m *license.Manager, logger *slog.Logger) *Licensing {
	if logger == nil {
		logger = slog.Default()
	}
	if m == nil {
		return &Licensing{err: ErrLicenseNotConfigured, logger: logger}
	}
	return &Licensing{m: m, logger: logger}
}

// Run re-checks the keys in use once a day until ctx is done.
func (l *Licensing) Run(ctx context.Context) {
	if l != nil && l.m != nil {
		l.m.Run(ctx)
	}
}

// Err is the configuration error, nil when licensing works.
func (l *Licensing) Err() error {
	if l == nil {
		return ErrLicenseNotConfigured
	}
	return l.err
}

// InstanceID is this deployment's id on the license server ("" when unconfigured).
func (l *Licensing) InstanceID() string {
	if l == nil || l.m == nil {
		return ""
	}
	return l.m.InstanceID()
}

// Entitlement returns the cached (checked once a day) entitlement of key. A
// refused license is returned together with a *LicenseError carrying the reason.
func (l *Licensing) Entitlement(ctx context.Context, key string) (*license.Entitlement, error) {
	if l == nil || l.m == nil {
		err := ErrLicenseNotConfigured
		if l != nil && l.err != nil {
			err = l.err
		}
		return nil, &LicenseError{Code: LicenseNotConfigured, Err: err}
	}
	ent, err := l.m.Entitlement(ctx, key)
	if err != nil {
		code := LicenseOffline
		switch {
		case errors.Is(err, license.ErrNoKey):
			code = LicenseNoKey
		case errors.Is(err, license.ErrInvalidKey):
			code = LicenseInvalidKey
		}
		return nil, &LicenseError{Code: code, Err: err}
	}
	if !ent.Access {
		le := &LicenseError{Code: ent.ReasonText(), Err: ent.Err()}
		if ent.Message != nil {
			le.Message = *ent.Message
		}
		return ent, le
	}
	return ent, nil
}

// Resolve checks key and picks its connection: by id or name when ref is set,
// otherwise the only one. A key with several connections and no ref answers
// LicenseChooseConnection with the list.
func (l *Licensing) Resolve(ctx context.Context, key, ref string) (*license.Entitlement, license.Connection, error) {
	ent, err := l.Entitlement(ctx, key)
	if err != nil {
		return ent, license.Connection{}, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" && len(ent.Connections) > 1 {
		_, cerr := ent.Connection("")
		return ent, license.Connection{}, &LicenseError{Code: LicenseChooseConnection, Connections: ent.Connections, Err: cerr}
	}
	conn, err := ent.Connection(ref)
	if err != nil {
		return ent, license.Connection{}, &LicenseError{Code: LicenseConnection, Err: err}
	}
	return ent, conn, nil
}

// ConnectionURL is the Vchasno host of a license connection: its "url"
// parameter, or the production host when the parameter is empty.
func ConnectionURL(c license.Connection) string {
	if u := c.URL(); u != "" {
		return u
	}
	return config.DefaultAPIBase
}

// Login is what a user supplies: the license key, optionally which of its
// connections to use, and their own Vchasno integration token. The token is
// never sent to the license server.
type Login struct {
	LicenseKey string
	Connection string // connection id or name; empty = the only one
	Token      string
}

// Open is the licensed entry point: it confirms the license, takes the Vchasno
// host from the chosen connection and validates the token against that host.
func Open(ctx context.Context, cfg config.Config, lic *Licensing, login Login, logger *slog.Logger) (*Session, error) {
	if logger == nil {
		logger = slog.Default()
	}
	login.LicenseKey = strings.TrimSpace(login.LicenseKey)
	login.Connection = strings.TrimSpace(login.Connection)
	login.Token = strings.TrimSpace(login.Token)
	if lic == nil || lic.m == nil {
		_, err := lic.Entitlement(ctx, login.LicenseKey)
		return nil, err
	}
	if login.LicenseKey == "" {
		return nil, &LicenseError{Code: LicenseNoKey,
			Err: fmt.Errorf("%w (the login page's license key, the X-License-Key header or MCP_LICENSE_KEY)", license.ErrNoKey)}
	}
	ent, conn, err := lic.Resolve(ctx, login.LicenseKey, login.Connection)
	if err != nil {
		return nil, err
	}
	st, err := dial(ctx, cfg, vchasno.Credentials{BaseURL: ConnectionURL(conn), Token: login.Token}, logger)
	if err != nil {
		return nil, err
	}
	s := &Session{Cfg: cfg, Logger: logger, OpenedAt: time.Now(), lic: lic,
		login:  Login{LicenseKey: login.LicenseKey, Connection: conn.ID, Token: login.Token},
		connID: conn.ID, connName: conn.Name, lastEnt: ent}
	s.state.Store(st)
	logger.Info("licensed session opened", "license", ent.LicenseID, "connection", conn.Name, "host", st.creds.BaseURL, "token", st.creds.Redacted())
	return s, nil
}

// Login returns what the session was opened with, the connection pinned to its
// id. The OAuth server encrypts it into the access token.
func (s *Session) Login() Login { return s.login }

// Licensed is false only for sessions opened with Connect (tests, mock).
func (s *Session) Licensed() bool { return s.lic != nil }

// CheckLicense is the gate every MCP request, REST bridge call and download
// link passes through. It is cheap: the entitlement is cached and re-checked
// with the license server once a day. A refused or unconfirmed license, or a
// connection that is gone from the license, is an error; a connection whose
// URL changed is followed by reconnecting to the new host in place.
func (s *Session) CheckLicense(ctx context.Context) error {
	if s.lic == nil {
		return nil
	}
	ent, conn, err := s.lic.Resolve(ctx, s.login.LicenseKey, s.connID)
	s.licMu.Lock()
	if ent != nil {
		s.lastEnt = ent
	}
	s.lastLicErr = err
	if err == nil {
		s.connName = conn.Name
	}
	name := s.connName
	s.licMu.Unlock()
	if err != nil {
		if le, ok := AsLicenseError(err); ok && le.Code == LicenseConnection {
			return &LicenseError{Code: LicenseConnection,
				Err: fmt.Errorf("the license connection %q is no longer available (%v); log in again and choose another one", name, le.Err)}
		}
		return err
	}
	url := ConnectionURL(conn)
	if url == s.Credentials().BaseURL {
		return nil
	}
	return s.rebind(ctx, url)
}

// rebind reconnects the session to a new host after the license moved it.
func (s *Session) rebind(ctx context.Context, url string) error {
	s.rebindMu.Lock()
	defer s.rebindMu.Unlock()
	old := s.Credentials().BaseURL
	if old == url {
		return nil
	}
	st, err := dial(ctx, s.Cfg, vchasno.Credentials{BaseURL: url, Token: s.login.Token}, s.Logger)
	if err != nil {
		s.Logger.Warn("license connection moved, reconnect failed", "from", old, "to", url, "err", err)
		return fmt.Errorf("the license moved this connection from %s to %s and the new host refused the token: %w", old, url, err)
	}
	s.state.Store(st)
	s.Invalidate()
	s.Logger.Info("license connection moved, session reconnected", "from", old, "to", url)
	return nil
}

// LicenseStatus is what self_check reports about the license. It never
// contains the license key.
type LicenseStatus struct {
	Licensed     bool     `json:"licensed"`
	OK           bool     `json:"ok"`
	LicenseID    string   `json:"license_id,omitempty"`
	Type         string   `json:"type,omitempty"`
	Status       string   `json:"status,omitempty"`
	Customer     string   `json:"customer,omitempty"`
	Plan         string   `json:"plan,omitempty"`
	PaidUntil    string   `json:"paid_until,omitempty"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
	ValidUntil   string   `json:"valid_until,omitempty"`
	NextCheck    string   `json:"next_check,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	Connection   string   `json:"connection,omitempty"`
	ConnectionID string   `json:"connection_id,omitempty"`
	Host         string   `json:"host,omitempty"`
	InstanceID   string   `json:"instance_id,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Message      string   `json:"message,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// LicenseStatus reports the outcome of the most recent license check.
func (s *Session) LicenseStatus() LicenseStatus {
	if s.lic == nil {
		return LicenseStatus{Licensed: false, OK: true, Host: s.Credentials().BaseURL,
			Message: "this session was opened without a license (test / mock mode)"}
	}
	s.licMu.Lock()
	ent, lerr, name, id := s.lastEnt, s.lastLicErr, s.connName, s.connID
	s.licMu.Unlock()
	out := LicenseStatus{Licensed: true, OK: lerr == nil, Connection: name, ConnectionID: id,
		Host: s.Credentials().BaseURL, InstanceID: s.lic.InstanceID()}
	if ent != nil {
		out.LicenseID, out.Type, out.Status = ent.LicenseID, ent.LicenseType, ent.LicenseStatus
		out.Customer, out.Plan = deref(ent.CustomerName), deref(ent.PlanName)
		out.PaidUntil, out.ExpiresAt = deref(ent.PaidUntil), deref(ent.ExpiresAt)
		out.ValidUntil = ent.ValidUntil.UTC().Format(time.RFC3339)
		out.NextCheck = ent.CheckAfter.UTC().Format(time.RFC3339)
		out.Warnings = ent.Warnings
		out.Reason, out.Message = ent.ReasonText(), deref(ent.Message)
	}
	if lerr != nil {
		out.Error = lerr.Error()
		if le, ok := AsLicenseError(lerr); ok && out.Reason == "" {
			out.Reason = le.Code
		}
	}
	return out
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
