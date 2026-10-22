// Command luma-server runs the Luma backend.
//
//	luma-server                          serve (migrates on startup)
//	luma-server healthcheck              exit 0 if /v1/health/ready is ok (container healthcheck)
//	luma-server admin invite             print a new single-use invite code
//	luma-server admin disable-user NAME  disable an account and revoke its sessions
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

	"luma.folk3s.com/server/internal/api"
	"luma.folk3s.com/server/internal/config"
	"luma.folk3s.com/server/internal/db"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, args []string) error {
	cfg, err := config.Load()
	if len(args) > 0 && args[0] == "healthcheck" {
		return healthcheck(cfg.Listen)
	}
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	version, err := db.Migrate(ctx, pool)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := os.MkdirAll(cfg.UploadDir, 0o700); err != nil {
		return fmt.Errorf("upload dir: %w", err)
	}
	app := api.New(api.Deps{Pool: pool, Config: cfg, Logger: logger})
	defer app.Close()

	if len(args) > 0 && args[0] == "admin" {
		return admin(ctx, app, args[1:])
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: it would cut long-lived WebSocket connections.
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("luma-server listening", "addr", cfg.Listen, "migration", version, "registration", cfg.Registration)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	app.Close() // tell sockets to reconnect; they will replay from their cursor
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func admin(ctx context.Context, app *api.App, args []string) error {
	switch {
	case len(args) == 1 && args[0] == "invite":
		code, err := app.Auth.CreateAdminInvite(ctx)
		if err != nil {
			return err
		}
		fmt.Println(code)
		return nil
	case len(args) == 2 && args[0] == "disable-user":
		if err := app.Auth.DisableUser(ctx, args[1]); err != nil {
			return err
		}
		fmt.Printf("disabled %s and revoked all sessions\n", args[1])
		return nil
	}
	return errors.New("usage: luma-server admin invite | admin disable-user <username>")
}

func healthcheck(listen string) error {
	host := listen
	if strings.HasPrefix(host, ":") || strings.HasPrefix(host, "0.0.0.0:") {
		host = "127.0.0.1" + host[strings.Index(host, ":"):]
	}
	c := http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://" + host + "/v1/health/ready")
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ready: %d", res.StatusCode)
	}
	return nil
}
