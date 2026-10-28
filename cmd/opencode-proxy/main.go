// Command opencode-proxy exposes the OpenCode free tier through an
// OpenAI-compatible HTTP surface on port 20144.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"opencode-proxy/internal/config"
	"opencode-proxy/internal/proxy"
)

// version is overridable at build time: -ldflags "-X main.version=1.2.3".
var version = "0.1.0"

func main() {
	var (
		configPath  = flag.String("config", "", "path to config.json (default: $OPENCODE_PROXY_CONFIG or ./config.json)")
		hostFlag    = flag.String("host", "", "listen host override")
		portFlag    = flag.Int("port", 0, "listen port override")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("opencode-proxy %s\n", version)
		return
	}

	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmsgprefix)
	logger.SetPrefix("opencode-proxy | ")

	resolvedPath := *configPath
	if resolvedPath == "" {
		resolvedPath = os.Getenv(config.EnvConfigPath)
	}
	manager, err := config.NewManager(resolvedPath)
	if err != nil {
		logger.Fatalf("configuration error: %v", err)
	}
	cfg := manager.Get()

	addr := net.JoinHostPort(hostFlagOr(cfg.Host, *hostFlag), fmt.Sprint(portOr(cfg.Port, *portFlag)))

	server := proxy.NewServer(manager, logger)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: responses stream for minutes.
	}

	logger.Printf("listening on http://%s (config=%s, default_model=%s, upstream=%s)",
		addr, manager.Path(), cfg.DefaultModel, cfg.UpstreamBaseURL)
	logger.Printf("endpoints: POST /v1/chat/completions, GET /v1/models, GET /health, GET|PUT /api/config")

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		logger.Fatalf("server error: %v", err)
	case sig := <-stop:
		logger.Printf("received %s, shutting down", sig)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
	}
	logger.Printf("stopped")
}

func hostFlagOr(configured, override string) string {
	if override != "" {
		return override
	}
	if configured != "" {
		return configured
	}
	return "127.0.0.1"
}

func portOr(configured, override int) int {
	if override > 0 {
		return override
	}
	if configured > 0 {
		return configured
	}
	return 20144
}
