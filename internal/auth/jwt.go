package auth

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TokenManager issues and validates access tokens.
//
// Token = JWE(RSA-OAEP-256, A256GCM) wrapping JWS(RS256) wrapping claims.
// The signature proves the token was issued by this server; the encryption
// hides the embedded Vchasno API token from the MCP client that stores it.
type TokenManager struct {
	issuer string
	key    *rsa.PrivateKey
	expiry time.Duration
}

// Credentials are the claims recovered from a valid token.
type Credentials struct {
	BaseURL string
	Token   string
	Subject string
	Expires time.Time
}

// NewTokenManager creates a manager for the given issuer and key.
func NewTokenManager(issuer string, key *rsa.PrivateKey, expiryDays int) *TokenManager {
	if expiryDays <= 0 {
		expiryDays = 365
	}
	return &TokenManager{issuer: issuer, key: key, expiry: time.Duration(expiryDays) * 24 * time.Hour}
}

// ExpirySeconds is the token lifetime for the /token response.
func (m *TokenManager) ExpirySeconds() int64 { return int64(m.expiry / time.Second) }

type claims struct {
	Iss     string `json:"iss"`
	Sub     string `json:"sub"`
	BaseURL string `json:"base_url"`
	Tok     string `json:"tok"`
	Iat     int64  `json:"iat"`
	Exp     int64  `json:"exp"`
	Jti     string `json:"jti"`
}

var b64 = base64.RawURLEncoding

// CreateToken builds a signed and encrypted token carrying the credentials.
// The subject is a fingerprint of the Vchasno token, never the token itself,
// so that logs and client-side introspection cannot leak it.
func (m *TokenManager) CreateToken(baseURL, apiToken string) (string, error) {
	now := time.Now()
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	c := claims{Iss: m.issuer, Sub: Fingerprint(apiToken), BaseURL: baseURL, Tok: apiToken,
		Iat: now.Unix(), Exp: now.Add(m.expiry).Unix(), Jti: b64.EncodeToString(jti)}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	jws, err := m.sign(payload)
	if err != nil {
		return "", err
	}
	return m.encrypt([]byte(jws))
}

// Validate decrypts, verifies and checks a token.
func (m *TokenManager) Validate(token string) (*Credentials, error) {
	inner, err := m.decrypt(strings.TrimSpace(token))
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	payload, err := m.verify(string(inner))
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, err
	}
	if c.Iss != m.issuer {
		return nil, fmt.Errorf("issuer mismatch")
	}
	exp := time.Unix(c.Exp, 0)
	if c.Exp != 0 && time.Now().After(exp) {
		return nil, fmt.Errorf("token expired at %s", exp.Format(time.RFC3339))
	}
	if c.BaseURL == "" || c.Tok == "" {
		return nil, fmt.Errorf("token missing credentials")
	}
	return &Credentials{BaseURL: c.BaseURL, Token: c.Tok, Subject: c.Sub, Expires: exp}, nil
}

// ── JWS (RS256, compact) ────────────────────────────────────────

func (m *TokenManager) sign(payload []byte) (string, error) {
	header := b64.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	signingInput := header + "." + b64.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, m.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + b64.EncodeToString(sig), nil
}

func (m *TokenManager) verify(jws string) ([]byte, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed JWS")
	}
	hdr, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var h struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(hdr, &h); err != nil || h.Alg != "RS256" {
		return nil, errors.New("unsupported JWS algorithm")
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&m.key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		return nil, errors.New("bad signature")
	}
	return b64.DecodeString(parts[1])
}

// ── JWE (RSA-OAEP-256 + A256GCM, compact) ───────────────────────

func (m *TokenManager) encrypt(plaintext []byte) (string, error) {
	cek := make([]byte, 32)
	if _, err := rand.Read(cek); err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	protected := b64.EncodeToString([]byte(`{"alg":"RSA-OAEP-256","enc":"A256GCM","cty":"JWT"}`))
	encKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, &m.key.PublicKey, cek, nil)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, plaintext, []byte(protected))
	tagStart := len(sealed) - gcm.Overhead()
	ciphertext, tag := sealed[:tagStart], sealed[tagStart:]
	return strings.Join([]string{protected, b64.EncodeToString(encKey), b64.EncodeToString(iv), b64.EncodeToString(ciphertext), b64.EncodeToString(tag)}, "."), nil
}

func (m *TokenManager) decrypt(token string) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 5 {
		return nil, errors.New("malformed JWE")
	}
	hdr, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var h struct {
		Alg string `json:"alg"`
		Enc string `json:"enc"`
	}
	if err := json.Unmarshal(hdr, &h); err != nil || h.Alg != "RSA-OAEP-256" || h.Enc != "A256GCM" {
		return nil, errors.New("unsupported JWE algorithm")
	}
	encKey, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	iv, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	ciphertext, err := b64.DecodeString(parts[3])
	if err != nil {
		return nil, err
	}
	tag, err := b64.DecodeString(parts[4])
	if err != nil {
		return nil, err
	}
	cek, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, m.key, encKey, nil)
	if err != nil {
		return nil, errors.New("cannot unwrap content key")
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, iv, append(ciphertext, tag...), []byte(parts[0]))
}

// Fingerprint is a short, stable, non-reversible identifier of an API token,
// safe to write into logs and into the token's `sub` claim.
func Fingerprint(apiToken string) string {
	sum := sha256.Sum256([]byte(apiToken))
	return b64.EncodeToString(sum[:9])
}
