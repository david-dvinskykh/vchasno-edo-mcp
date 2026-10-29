// Package license is this server's copy of the license server client, protocol v2
// (docs/PROTOCOL.md). It depends only on the standard library so a product can
// copy it verbatim (as internal/license) instead of importing this module.
//
// Usage:
//
//	m, err := licclient.NewManager(licclient.Config{
//		ServerURL: "https://license.example", PublicKey: "<base64 X.509>",
//		Product: "enote-mcp", AppVersion: version, StateDir: dataDir,
//	})
//	go m.Run(ctx)                    // daily re-checks of every key in use
//	ent, err := m.Entitlement(ctx, key) // cached; checks when due
//	if err != nil { /* offline past validUntil, invalid key, ... */ }
//	if !ent.Access { /* refused: ent.Reason */ }
//	conn, err := ent.Connection("")  // the only one, or by id/name
package license

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ProtocolVersion is the version of the v2 check protocol.
const ProtocolVersion = 2

// Errors returned by Check and Entitlement.
var (
	// ErrInvalidKey: the server does not know the key (or it was revoked).
	ErrInvalidKey = errors.New("license key is not valid")
	// ErrOffline: no successful check within the validity of the last one.
	ErrOffline = errors.New("license server unreachable: the license could not be confirmed in the last 24 hours")
	// ErrNoKey: the caller passed an empty key.
	ErrNoKey = errors.New("license key is required")
)

// Config configures a Manager.
type Config struct {
	ServerURL  string // base URL of the license server, e.g. https://license.example
	PublicKey  string // base64 X.509 SubjectPublicKeyInfo of the Ed25519 signing key
	Product    string // product id, e.g. "enote-mcp"
	AppVersion string
	// StateDir keeps the instance id and the cached signed payloads. Empty = no persistence
	// (a random instance id per process start).
	StateDir string
	// RetryInterval between failed checks (default 5 minutes).
	RetryInterval time.Duration
	HTTPClient    *http.Client
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Connection is one connection record of the license.
type Connection struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Params map[string]string `json:"params"`
}

// URL is the conventional "url" parameter.
func (c Connection) URL() string { return strings.TrimRight(c.Params["url"], "/") }

// Entitlement is a verified v2 payload.
type Entitlement struct {
	ProtocolVersion int          `json:"protocolVersion"`
	LicenseID       string       `json:"licenseId"`
	Product         string       `json:"product"`
	InstanceID      string       `json:"instanceId"`
	Nonce           string       `json:"nonce"`
	IssuedAt        time.Time    `json:"issuedAt"`
	CheckAfter      time.Time    `json:"checkAfter"`
	ValidUntil      time.Time    `json:"validUntil"`
	Access          bool         `json:"access"`
	Reason          *string      `json:"reason"`
	Warnings        []string     `json:"warnings"`
	LicenseType     string       `json:"licenseType"`
	LicenseStatus   string       `json:"licenseStatus"`
	CustomerName    *string      `json:"customerName"`
	PlanCode        *string      `json:"planCode"`
	PlanName        *string      `json:"planName"`
	PaidUntil       *string      `json:"paidUntil"`
	ExpiresAt       *string      `json:"expiresAt"`
	Message         *string      `json:"message"`
	Connections     []Connection `json:"connections"`
}

// ReasonText is Reason or "" when access is granted.
func (e *Entitlement) ReasonText() string {
	if e == nil || e.Reason == nil {
		return ""
	}
	return *e.Reason
}

// Err is nil when access is granted, otherwise a human-readable refusal.
func (e *Entitlement) Err() error {
	if e.Access {
		return nil
	}
	msg := "license does not allow access: " + e.ReasonText()
	if e.Message != nil && *e.Message != "" {
		msg += " (" + *e.Message + ")"
	}
	return errors.New(msg)
}

// Connection picks a connection: by id or name (case-insensitive) when ref is set,
// otherwise the only one. With several connections and no ref it returns an error
// listing the names.
func (e *Entitlement) Connection(ref string) (Connection, error) {
	if err := e.Err(); err != nil {
		return Connection{}, err
	}
	ref = strings.TrimSpace(ref)
	if ref != "" {
		for _, c := range e.Connections {
			if c.ID == ref || strings.EqualFold(c.Name, ref) {
				return c, nil
			}
		}
		return Connection{}, fmt.Errorf("the license has no connection %q", ref)
	}
	switch len(e.Connections) {
	case 0:
		return Connection{}, errors.New("the license has no connection for this product")
	case 1:
		return e.Connections[0], nil
	}
	names := make([]string, 0, len(e.Connections))
	for _, c := range e.Connections {
		names = append(names, c.Name)
	}
	return Connection{}, fmt.Errorf("the license has several connections, choose one of: %s", strings.Join(names, ", "))
}

type envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
	KeyID     string `json:"keyId"`
}

type entry struct {
	key       string
	ent       *Entitlement
	env       envelope
	lastError error
	nextTry   time.Time
}

// Manager checks license keys and caches the verified entitlements.
type Manager struct {
	cfg        Config
	pub        ed25519.PublicKey
	instanceID string
	client     *http.Client

	mu       sync.Mutex
	entries  map[string]*entry // by key hash
	inFlight map[string]*sync.Mutex
}

var instanceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// NewManager validates the configuration, loads the instance id and the cache.
func NewManager(cfg Config) (*Manager, error) {
	cfg.ServerURL = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	if cfg.ServerURL == "" {
		return nil, errors.New("license server URL is not configured")
	}
	if cfg.Product == "" {
		return nil, errors.New("product is required")
	}
	pub, err := ParsePublicKey(cfg.PublicKey)
	if err != nil {
		return nil, err
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 5 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	m := &Manager{cfg: cfg, pub: pub, client: client, entries: map[string]*entry{}, inFlight: map[string]*sync.Mutex{}}
	if m.instanceID, err = m.loadInstanceID(); err != nil {
		return nil, err
	}
	m.loadCache()
	return m, nil
}

// ParsePublicKey decodes a base64 X.509 SubjectPublicKeyInfo Ed25519 key (or a raw
// 32-byte key in base64).
func ParsePublicKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw) == 0 {
		return nil, errors.New("license public key is missing or not base64")
	}
	if len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	k, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("license public key: %w", err)
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("license public key is not Ed25519")
	}
	return pub, nil
}

// InstanceID is this deployment's stable id.
func (m *Manager) InstanceID() string { return m.instanceID }

// KeyHash is the hex SHA-256 of a trimmed key, the form used in the cache.
func KeyHash(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

// Entitlement returns the entitlement for key: the cached one while it is fresh
// (before checkAfter), otherwise a new check. When the check fails the cached
// payload is still returned until validUntil; after that ErrOffline (or the
// check error). A refused license is returned with Access=false and no error.
func (m *Manager) Entitlement(ctx context.Context, key string) (*Entitlement, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, ErrNoKey
	}
	h := KeyHash(key)
	now := m.cfg.Now()

	m.mu.Lock()
	e := m.entries[h]
	if e != nil {
		e.key = key
		if e.ent != nil && now.Before(e.ent.CheckAfter) {
			ent := e.ent
			m.mu.Unlock()
			return ent, nil
		}
		if now.Before(e.nextTry) {
			// A check failed recently: do not hammer the server before the retry time.
			if e.ent != nil && now.Before(e.ent.ValidUntil) {
				ent := e.ent
				m.mu.Unlock()
				return ent, nil
			}
			err := e.lastError
			m.mu.Unlock()
			return nil, fmt.Errorf("%w (%v)", ErrOffline, err)
		}
	}
	lock := m.inFlight[h]
	if lock == nil {
		lock = &sync.Mutex{}
		m.inFlight[h] = lock
	}
	m.mu.Unlock()

	lock.Lock()
	defer lock.Unlock()
	// Another caller may have refreshed it meanwhile.
	m.mu.Lock()
	if e = m.entries[h]; e != nil && e.ent != nil && m.cfg.Now().Before(e.ent.CheckAfter) {
		ent := e.ent
		m.mu.Unlock()
		return ent, nil
	}
	m.mu.Unlock()

	ent, env, err := m.check(ctx, key)
	m.mu.Lock()
	defer m.mu.Unlock()
	e = m.entries[h]
	if e == nil {
		e = &entry{key: key}
		m.entries[h] = e
	}
	if err == nil {
		e.ent, e.env, e.lastError, e.nextTry = ent, env, nil, time.Time{}
		m.saveCacheLocked()
		return ent, nil
	}
	e.lastError = err
	e.nextTry = m.cfg.Now().Add(m.cfg.RetryInterval)
	if errors.Is(err, ErrInvalidKey) {
		delete(m.entries, h)
		m.saveCacheLocked()
		return nil, err
	}
	if e.ent != nil && m.cfg.Now().Before(e.ent.ValidUntil) {
		return e.ent, nil
	}
	return nil, fmt.Errorf("%w (%v)", ErrOffline, err)
}

// Forget drops a key from the cache (e.g. on logout).
func (m *Manager) Forget(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, KeyHash(key))
	m.saveCacheLocked()
}

// LastError of the key's most recent failed check, if any.
func (m *Manager) LastError(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[KeyHash(key)]; e != nil {
		return e.lastError
	}
	return nil
}

// Run re-checks every key in use when it is due, until ctx is done. Keys loaded from
// the cache are re-checked only after they were used once in this process (the clear
// key is not persisted).
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := m.cfg.Now()
		var due []string
		m.mu.Lock()
		for _, e := range m.entries {
			if e.key == "" {
				continue
			}
			if (e.ent == nil || !now.Before(e.ent.CheckAfter)) && !now.Before(e.nextTry) {
				due = append(due, e.key)
			}
		}
		m.mu.Unlock()
		for _, k := range due {
			cctx, cancel := context.WithTimeout(ctx, time.Minute)
			_, _ = m.Entitlement(cctx, k)
			cancel()
		}
	}
}

// Check performs one network check of key, without the cache.
func (m *Manager) Check(ctx context.Context, key string) (*Entitlement, error) {
	ent, _, err := m.check(ctx, strings.TrimSpace(key))
	return ent, err
}

func (m *Manager) check(ctx context.Context, key string) (*Entitlement, envelope, error) {
	if key == "" {
		return nil, envelope{}, ErrNoKey
	}
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, envelope{}, err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	host, _ := os.Hostname()
	body, _ := json.Marshal(map[string]any{
		"protocolVersion": ProtocolVersion,
		"product":         m.cfg.Product,
		"instanceId":      m.instanceID,
		"nonce":           nonce,
		"appVersion":      m.cfg.AppVersion,
		"hostname":        host,
		"sentAt":          m.cfg.Now().UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.cfg.ServerURL+"/api/v2/license/check", bytes.NewReader(body))
	if err != nil {
		return nil, envelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, envelope{}, fmt.Errorf("license check: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, envelope{}, fmt.Errorf("license check: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, envelope{}, ErrInvalidKey
	}
	if resp.StatusCode != http.StatusOK {
		return nil, envelope{}, fmt.Errorf("license check: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, envelope{}, fmt.Errorf("license check: bad response: %w", err)
	}
	ent, err := m.verify(env)
	if err != nil {
		return nil, envelope{}, err
	}
	if ent.Nonce != nonce {
		return nil, envelope{}, errors.New("license check: nonce mismatch")
	}
	return ent, env, nil
}

// verify checks the signature, product and instance of an envelope.
func (m *Manager) verify(env envelope) (*Entitlement, error) {
	payload, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return nil, errors.New("license check: payload is not base64")
	}
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil || !ed25519.Verify(m.pub, payload, sig) {
		return nil, errors.New("license check: signature is not valid")
	}
	var ent Entitlement
	if err := json.Unmarshal(payload, &ent); err != nil {
		return nil, fmt.Errorf("license check: payload: %w", err)
	}
	if ent.ProtocolVersion != ProtocolVersion || ent.Product != m.cfg.Product || ent.InstanceID != m.instanceID {
		return nil, errors.New("license check: payload is for another product or instance")
	}
	if ent.ValidUntil.IsZero() || ent.CheckAfter.IsZero() {
		return nil, errors.New("license check: payload has no validity")
	}
	return &ent, nil
}

func (m *Manager) loadInstanceID() (string, error) {
	random := func() string {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		return "inst_" + hex.EncodeToString(b)
	}
	if m.cfg.StateDir == "" {
		return random(), nil
	}
	path := filepath.Join(m.cfg.StateDir, "license-instance-id")
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); instanceIDPattern.MatchString(id) {
			return id, nil
		}
	}
	id := random()
	if err := os.MkdirAll(m.cfg.StateDir, 0o700); err != nil {
		return "", fmt.Errorf("license state dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("license instance id: %w", err)
	}
	return id, nil
}

func (m *Manager) cachePath() string {
	if m.cfg.StateDir == "" {
		return ""
	}
	return filepath.Join(m.cfg.StateDir, "license-cache-"+m.cfg.Product+".json")
}

func (m *Manager) loadCache() {
	path := m.cachePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var stored map[string]envelope
	if json.Unmarshal(data, &stored) != nil {
		return
	}
	now := m.cfg.Now()
	for h, env := range stored {
		ent, err := m.verify(env)
		if err != nil || !now.Before(ent.ValidUntil) {
			continue
		}
		m.entries[h] = &entry{ent: ent, env: env}
	}
}

func (m *Manager) saveCacheLocked() {
	path := m.cachePath()
	if path == "" {
		return
	}
	stored := map[string]envelope{}
	for h, e := range m.entries {
		if e.ent != nil {
			stored[h] = e.env
		}
	}
	data, _ := json.Marshal(stored)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}
