// Command vchasno-edo-mcp serves the Vchasno.EDO (Вчасно.ЕДО) MCP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/auth"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/config"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/httpserver"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/session"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/tools"
	"github.com/david-dvinskykh/vchasno-edo-mcp/internal/vchasno"
)

func main() {
	cfg := config.Load()
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("vchasno-edo-mcp starting", "version", tools.Version, "issuer", cfg.IssuerURL,
		"listen", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), "api", cfg.BaseURL, "read_only", cfg.ReadOnly)

	connect := func(ctx context.Context, creds vchasno.Credentials) (*session.Session, error) {
		return session.Connect(ctx, cfg, creds, logger)
	}

	var preAuth *session.Session
	if cfg.PreAuth() {
		logger.Info("pre-auth mode: connecting", "api", cfg.BaseURL)
		sess, err := connectWithRetry(ctx, connect, vchasno.Credentials{BaseURL: cfg.BaseURL, Token: cfg.Token}, logger)
		if err != nil {
			logger.Error("pre-auth connection failed", "err", err)
			os.Exit(1)
		}
		preAuth = sess
		if sess.APIOpen {
			logger.Info("pre-auth session ready")
		} else {
			// Not fatal: the server still answers self_check and get_billing,
			// which is exactly what somebody debugging this needs.
			logger.Warn("pre-auth session ready but the company's API is closed", "reason", sess.APINote)
		}
	} else {
		logger.Info("auth mode: OAuth 2.1 (JWT tokens) or credential headers", "headers", []string{cfg.HeaderURL, cfg.HeaderToken})
	}

	key, err := auth.LoadOrGenerateKey(cfg.JWTKeyDir, logger)
	if err != nil {
		logger.Error("cannot initialise RSA key", "err", err)
		os.Exit(1)
	}
	oauth := auth.NewServer(cfg.IssuerURL, cfg.IssuerURL+"/mcp", auth.NewTokenManager(cfg.IssuerURL, key, cfg.JWTExpiryDays), connect, logger)

	srv := httpserver.New(cfg, logger, oauth, preAuth)
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
	logger.Info("MCP endpoint ready", "url", cfg.IssuerURL+"/mcp")
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}

// connectWithRetry keeps trying the pre-auth connection: the network or the
// Vchasno host may be unavailable for a moment when the container starts.
// An invalid token is not retried — it will not become valid by waiting.
func connectWithRetry(ctx context.Context, connect func(context.Context, vchasno.Credentials) (*session.Session, error),
	creds vchasno.Credentials, logger *slog.Logger) (*session.Session, error) {
	delay := 3 * time.Second
	for attempt := 1; ; attempt++ {
		sess, err := connect(ctx, creds)
		if err == nil {
			return sess, nil
		}
		var ae *vchasno.Error
		if errors.As(err, &ae) && (ae.Status == 401 || ae.Code == "login_required") {
			return nil, err
		}
		if attempt >= 8 || ctx.Err() != nil {
			return nil, err
		}
		logger.Warn("connection attempt failed, retrying", "attempt", attempt, "delay", delay, "err", err)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}
