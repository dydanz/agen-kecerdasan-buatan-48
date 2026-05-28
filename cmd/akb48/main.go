// Package main is the akb48 entry point.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	cliadapter "github.com/dydanz/akb48/adapters/cli"
	dcadapter "github.com/dydanz/akb48/adapters/discord"
	tgadapter "github.com/dydanz/akb48/adapters/telegram"
	"github.com/dydanz/akb48/internal/config"
	"github.com/dydanz/akb48/internal/onboard"
	"github.com/dydanz/akb48/internal/runtime"
)

func main() {
	onboardFlag := flag.Bool("onboard", false, "detect CLI auth and print backend config guidance")
	configPath := flag.String("config", "config.toml", "path to config.toml")
	validate := flag.Bool("validate", false, "validate config and exit")
	flag.Parse()

	if *onboardFlag {
		onboard.Run()
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if *validate {
		fmt.Printf("config ok: model=%s adapter_cli=%v adapter_discord=%v adapter_telegram=%v\n",
			cfg.LLM.Model, cfg.Adapters.CLI.Enabled, cfg.Adapters.Discord.Enabled, cfg.Adapters.Telegram.Enabled)
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

	// CLI adapter (always started)
	if cfg.Adapters.CLI.Enabled {
		adapter := cliadapter.New(rt.HandleMessage)
		go func() {
			if err := adapter.Start(ctx); err != nil {
				slog.Error("CLI adapter error", "error", err)
			}
		}()
	}

	switch {
	case cfg.Adapters.Discord.Enabled && cfg.Adapters.Telegram.Enabled:
		slog.Warn("Both Discord and Telegram enabled — Discord takes precedence. Disable one in config.toml.")
		fallthrough
	case cfg.Adapters.Discord.Enabled:
		dc, err := dcadapter.New(cfg.Adapters.Discord, rt.HandleMessage)
		if err != nil {
			slog.Error("Discord adapter init failed", "error", err)
			os.Exit(1)
		}
		go func() {
			if err := dc.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("Discord adapter error", "error", err)
			}
		}()

	case cfg.Adapters.Telegram.Enabled:
		tg, err := tgadapter.New(cfg.Adapters.Telegram, rt.HandleMessage)
		if err != nil {
			slog.Error("Telegram adapter init failed", "error", err)
			os.Exit(1)
		}
		go func() {
			if err := tg.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("Telegram adapter error", "error", err)
			}
		}()
		slog.Info("Telegram adapter started (fallback mode)")
	}

	<-ctx.Done()
	slog.Info("Shutting down...")
	if err := rt.Stop(); err != nil {
		slog.Error("Shutdown error", "error", err)
	}
	slog.Info("Shutdown complete")
}
