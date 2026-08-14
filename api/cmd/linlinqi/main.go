package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/database"
	"linlinqi/api/internal/handler"
	"linlinqi/api/internal/queue"
	"linlinqi/api/internal/router"
	"linlinqi/api/internal/security"
)

func main() {
	mode := "all"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if err := run(mode); err != nil {
		slog.Error("LinLinQi stopped", "error", err)
		os.Exit(1)
	}
}

func run(mode string) error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	vault, err := security.NewVault(cfg.DataEncryptionKey)
	if err != nil {
		return err
	}
	resources, err := database.Connect(cfg)
	if err != nil {
		return err
	}
	if err := database.Migrate(resources.DB); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if cfg.BootstrapAdmin {
		if err := database.BootstrapAdmin(resources.DB, cfg); err != nil {
			return fmt.Errorf("bootstrap administrator: %w", err)
		}
	}
	if cfg.SeedData {
		if err := database.SeedDevelopmentData(resources.DB, cfg, vault); err != nil {
			return fmt.Errorf("seed development data: %w", err)
		}
	}
	if mode == "migrate" {
		return nil
	}

	var server *http.Server
	var worker *queue.Worker
	errCh := make(chan error, 2)
	if mode == "api" || mode == "all" {
		server = &http.Server{
			Addr: net.JoinHostPort(cfg.BindAddress, cfg.Port), Handler: router.New(cfg, resources.DB, resources.Redis, vault),
			ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second,
			WriteTimeout: 45 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 << 10,
		}
		go func() {
			slog.Info("LinLinQi API started", "address", server.Addr)
			if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				errCh <- serveErr
			}
		}()
	}
	if mode == "worker" || mode == "all" {
		importProcessor := handler.Handler{DB: resources.DB, Cfg: cfg, Vault: vault}
		worker = queue.NewWorker(cfg, resources.DB, vault, importProcessor.ProcessSupplierCatalogImportJob)
		go func() { errCh <- worker.Run() }()
	}
	if server == nil && worker == nil {
		return fmt.Errorf("unknown mode %q; use all, api, worker, or migrate", mode)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
		slog.Info("LinLinQi shutdown requested")
	case runErr := <-errCh:
		if runErr != nil {
			slog.Error("service failed", "error", runErr)
		}
	}
	if worker != nil {
		worker.Shutdown()
	}
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
	return nil
}
