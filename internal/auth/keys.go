// Package auth implements the OAuth 2.1 authorization server (dynamic client
// registration, PKCE, login page) and the signed + encrypted JWT access tokens
// that carry the user's Vchasno API token — the same authentication interface
// as bas-corp-mcp, so one MCP client configuration style covers both servers.
package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const keyFileName = "vchasno-edo-mcp-key.pem"

// LoadOrGenerateKey returns the server RSA key pair, creating and persisting
// it on first start so tokens survive restarts.
func LoadOrGenerateKey(dir string, logger *slog.Logger) (*rsa.PrivateKey, error) {
	if dir == "" {
		dir = "."
	}
	path := filepath.Join(dir, keyFileName)
	if data, err := os.ReadFile(path); err == nil {
		if key, err := parseKey(data); err == nil {
			logger.Info("loaded RSA key", "path", path)
			return key, nil
		} else {
			logger.Warn("key file is corrupted, generating a new one", "path", path, "err", err)
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.MkdirAll(dir, 0o700); err == nil {
		if err := os.WriteFile(path, block, 0o600); err != nil {
			logger.Warn("could not persist RSA key; tokens will not survive restart", "path", path, "err", err)
		} else {
			logger.Info("generated and saved new RSA key", "path", path)
		}
	} else {
		logger.Warn("could not create key dir; tokens will not survive restart", "dir", dir, "err", err)
	}
	return key, nil
}

func parseKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA key")
	}
	return key, nil
}
