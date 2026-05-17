// Package main is the akb48 entry point.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/dydanz/akb48/internal/config"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config file")
	validate := flag.Bool("validate", false, "validate config and exit")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	if *validate {
		fmt.Printf("config ok: model=%s adapter_cli=%v adapter_telegram=%v\n",
			cfg.LLM.Model, cfg.Adapters.CLI.Enabled, cfg.Adapters.Telegram.Enabled)
		return
	}

	// Runtime wiring is Phase 1 work (see akb48-dev-plan/phase-1-session-runtime.md).
	// This scaffold confirms config loads correctly; the daemon loop comes next.
	slog.Info("akb48 starting", "model", cfg.LLM.Model)
	slog.Warn("runtime not yet wired — start with --validate to confirm config")
	os.Exit(1)
}
