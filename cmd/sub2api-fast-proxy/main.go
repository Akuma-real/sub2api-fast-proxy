package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/akuma-real/sub2api-fast-proxy/internal/proxy"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "run a local health check and exit")
	flag.Parse()

	if *healthcheck {
		if err := runHealthcheck(); err != nil {
			_, _ = os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		return
	}

	logger := newLogger()

	cfg, err := proxy.LoadConfigFromEnv()
	if err != nil {
		logger.Error("invalid config", "error", err)
		os.Exit(2)
	}

	handler := proxy.NewHandler(cfg, logger)
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("sub2api fast proxy started",
			"listen", cfg.ListenAddr,
			"upstream", cfg.UpstreamURL.String(),
			"service_tier", cfg.ForceServiceTier,
			"max_body_bytes", cfg.MaxBodyBytes,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stopCh:
		logger.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		if err != nil {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("shutdown complete")
}

func runHealthcheck() error {
	target := strings.TrimSpace(os.Getenv("HEALTHCHECK_URL"))
	if target == "" {
		addr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
		if addr == "" {
			addr = ":8787"
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			if strings.HasPrefix(addr, ":") {
				port = strings.TrimPrefix(addr, ":")
			} else {
				port = "8787"
			}
		}
		if host == "" || host == "::" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		target = "http://" + net.JoinHostPort(host, port) + "/healthz"
	}

	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("healthcheck returned " + resp.Status)
	}
	return nil
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
