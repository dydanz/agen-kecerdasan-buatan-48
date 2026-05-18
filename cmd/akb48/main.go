// Package main is the akb48 entry point.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	cliadapter "github.com/dydanz/akb48/adapters/cli"
	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/runtime"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config.toml")
	validate := flag.Bool("validate", false, "validate config and exit")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if *validate {
		fmt.Printf("config ok: model=%s adapter_cli=%v adapter_telegram=%v\n",
			cfg.LLM.Model, cfg.Adapters.CLI.Enabled, cfg.Adapters.Telegram.Enabled)
		os.Exit(0)
	}

	rt, err := runtime.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtime init error: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rt.Start(ctx); err != nil {
		slog.Error("Start failed", "error", err)
		os.Exit(1)
	}

	adapter := cliadapter.New(rt.HandleMessage)
	go func() {
		if err := adapter.Start(ctx); err != nil {
			slog.Error("CLI adapter error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("Shutting down...")
	if err := rt.Stop(); err != nil {
		slog.Error("Shutdown error", "error", err)
	}
	slog.Info("Shutdown complete")
}
