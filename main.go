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
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := loadConfig()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	application := newServer(cfg, logger)
	httpServer := &http.Server{
		Addr:              cfg.Address,
		Handler:           application.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("mock ai api started",
		"address", cfg.Address,
		"model", cfg.Model,
		"vocabulary_size", len(modelVocabulary),
		"ttft_range", cfg.TTFT.String(),
		"tps_range", cfg.TPS.String(),
		"latency_range", cfg.Latency.String(),
		"output_tokens_range", cfg.OutputTokens.String(),
		"gomaxprocs", defaultMaxProcs(),
	)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
	logger.Info("mock ai api stopped")
}
