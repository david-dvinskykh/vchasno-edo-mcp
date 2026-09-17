// Package config reads server configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultAPIBase is the production host of Vchasno.EDO.
const DefaultAPIBase = "https://edo.vchasno.ua"

// Config holds every tunable of the MCP server. All values come from the
// environment so that the same binary works in Docker, systemd and a plain shell.
type Config struct {
	Host      string
	Port      int
	IssuerURL string // public base URL of this server, used in OAuth metadata and JWT issuer

	// Pre-auth mode: when Token is set, every MCP client shares one Vchasno
	// company context and OAuth is skipped entirely.
	BaseURL string
	Token   string

	// Header mode: MCP clients may pass credentials in these headers instead of OAuth.
	HeaderURL   string
	HeaderToken string

	JWTKeyDir     string
	JWTExpiryDays int

	// API safety limits.
	RequestTimeout time.Duration
	MaxRPS         float64 // Vchasno allows 10 requests per second per company
	MaxPageSize    int
	DefaultPage    int
	MaxPages       int // how many cursor pages one tool call may walk
	MaxUploadBytes int64
	RetryAttempts  int

	// File exchange.
	DownloadDir  string
	AllowUploads bool // allow reading files from the local filesystem for uploads
	ReadOnly     bool // hide every tool that changes data in Vchasno

	LogLevel string
}

// Load builds the config from the process environment.
func Load() Config {
	port := envInt("MCP_PORT", 8080)
	c := Config{
		Host:      env("MCP_HOST", "0.0.0.0"),
		Port:      port,
		IssuerURL: strings.TrimRight(env("MCP_ISSUER_URL", "http://localhost:"+strconv.Itoa(port)), "/"),

		BaseURL: strings.TrimRight(env("MCP_VCHASNO_URL", DefaultAPIBase), "/"),
		Token:   env("MCP_VCHASNO_TOKEN", ""),

		HeaderURL:   env("MCP_HEADER_URL", "X-Vchasno-URL"),
		HeaderToken: env("MCP_HEADER_TOKEN", "X-Vchasno-Token"),

		JWTKeyDir:     env("MCP_JWT_KEY_DIR", "."),
		JWTExpiryDays: envInt("MCP_JWT_EXPIRY_DAYS", 365),

		RequestTimeout: time.Duration(envInt("MCP_API_TIMEOUT_SEC", 90)) * time.Second,
		MaxRPS:         envFloat("MCP_MAX_RPS", 8),
		MaxPageSize:    envInt("MCP_MAX_PAGE_SIZE", 100),
		DefaultPage:    envInt("MCP_DEFAULT_PAGE_SIZE", 25),
		MaxPages:       envInt("MCP_MAX_PAGES", 20),
		MaxUploadBytes: int64(envInt("MCP_MAX_UPLOAD_MB", 15)) << 20,
		RetryAttempts:  envInt("MCP_RETRY_ATTEMPTS", 3),

		DownloadDir:  env("MCP_DOWNLOAD_DIR", os.TempDir()),
		AllowUploads: envBool("MCP_ALLOW_LOCAL_FILES", true),
		ReadOnly:     envBool("MCP_READ_ONLY", false),

		LogLevel: env("MCP_LOG_LEVEL", "info"),
	}
	if c.DefaultPage > c.MaxPageSize {
		c.DefaultPage = c.MaxPageSize
	}
	return c
}

// PreAuth reports whether the server runs with a single shared company token.
func (c Config) PreAuth() bool { return c.Token != "" }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && f > 0 {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}
