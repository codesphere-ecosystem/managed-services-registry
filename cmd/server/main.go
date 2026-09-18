// Copyright (c) Codesphere SE
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/codesphere-cloud/managed-services-lib/api"
	msconfig "github.com/codesphere-cloud/managed-services-lib/config"
	"github.com/codesphere-cloud/managed-services-lib/provider"

	"github.com/codesphere-ecosystem/managed-services-registry/internal/harbor"
)

func main() {
	cfg, err := msconfig.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	harborConfig, err := harbor.LoadConfigFromEnv()
	if err != nil {
		slog.Error("failed to load harbor config", "error", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.IsProduction())
	slog.SetDefault(logger)

	harborClient, err := harbor.NewClient(harborConfig)
	if err != nil {
		logger.Error("failed to create harbor client", "error", err)
		os.Exit(1)
	}

	harborProvider := harbor.NewProvider(harborConfig, harborClient, logger)

	routes := map[string]func(*gin.RouterGroup){
		"harbor": func(group *gin.RouterGroup) {
			provider.RegisterRoutes(group, harborProvider)
		},
	}

	server, err := api.NewServer(cfg, routes)
	if err != nil {
		logger.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run()
	}()

	logger.Info("server started", "port", cfg.Port, "provider", "harbor")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	case <-quit:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
}

func newLogger(production bool) *slog.Logger {
	if production {
		return slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return slog.New(slog.NewTextHandler(os.Stdout, nil))
}
