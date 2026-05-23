# AKB48 — Development Plan

**Last Updated:** 2026-05-23
**Language:** Go 1.25
**Status:** All phases complete — Hello World milestone shipped

---

## What Is AKB48

Thin, self-hosted AI agent runtime for a solo operator. Connects Telegram (and CLI) to Claude via a knowledge brain (GBrain). Gets smarter without code deploys — intelligence lives in skill files and GBrain, not in the runtime.

**Philosophy:** "thin harness, fat skills" — ~2,500 lines of Go runtime, behavior in markdown.

**Stack:**
- Runtime: Go 1.25, `anthropic-sdk-go`, `go-telegram-bot-api/v5`
- Brain: GBrain over stdio MCP (JSON-RPC 2.0), PostgreSQL + pgvector
- Config: TOML, secrets via env vars only
- Deployment: VPS (Hetzner), systemd, Docker (GBrain + Postgres)

---

## Quick Start (CLI mode, no Telegram)

```bash
# 1. Clone and build
git clone https://github.com/dydanz/agen-kecerdasan-buatan-48
cd agen-kecerdasan-buatan-48
go build -o akb48 ./cmd/akb48/

# 2. Set required env var
export ANTHROPIC_API_KEY=sk-ant-...

# 3. Validate config
./akb48 --validate

# 4. Run (CLI adapter enabled by default)
./akb48
# > hello
# > remember that staging cluster is ap-southeast-1
# > what do you know about staging?
```

---

## Enable Telegram

1. Create a bot via [@BotFather](https://t.me/botfather), get `TELEGRAM_BOT_TOKEN`
2. Find your Telegram user ID (e.g. via [@userinfobot](https://t.me/userinfobot))
3. Edit `config.toml`:

```toml
[adapters.telegram]
enabled = true
token_env = "TELEGRAM_BOT_TOKEN"
allowed_user_ids = [YOUR_USER_ID]   # only you can use it
streaming_interval_ms = 1000
```

4. Set env var and run:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
export TELEGRAM_BOT_TOKEN=...
./akb48
```

---

## Enable GBrain (Knowledge Brain)

GBrain must run as a separate process. AKB48 connects via MCP stdio.

```bash
# Terminal 1: start GBrain
gbrain serve

# Terminal 2: enable brain in config.toml
# [brain]
# enabled = true
# gbrain_command = "gbrain"
# gbrain_args = ["serve"]
# gbrain_working_dir = "~/brain"

./akb48
# Brain connected → gbrain_search, gbrain_put tools registered
# "remember that X" → stored in GBrain
# "what do you know about X?" → retrieved from GBrain
```

If GBrain is unavailable, AKB48 continues in degraded mode (no brain tools, LLM still responds).

---

## VPS Deployment

Requires: Hetzner VPS (or any Linux), Docker, systemd.

```bash
# 1. Set VPS IP
export VPS_HOST=<your-vps-ip>

# 2. First-time setup (copies systemd unit, creates dirs)
make setup-vps

# 3. Create .env on VPS
ssh dandi@$VPS_HOST "cat > ~/.env << 'EOF'
ANTHROPIC_API_KEY=sk-ant-...
TELEGRAM_BOT_TOKEN=...
EOF"

# 4. Start GBrain stack on VPS
ssh dandi@$VPS_HOST "cd ~/akb48 && docker compose -f deploy/docker-compose.yml up -d"

# 5. Deploy binary + config
make deploy

# 6. Check status
make logs
# Expected: "AKB48 initialised brain=connected(32 tools) skills=2"
```

**Redeploy after changes:**
```bash
make deploy   # builds locally, runs tests, rsyncs to VPS, restarts service
```

**Health check:**
```bash
ssh dandi@$VPS_HOST "~/akb48/scripts/healthcheck.sh"
# Expected: "All systems operational"
```

---

## Add a New Skill

No code changes. Create a directory and SKILL.md:

```bash
mkdir -p skills/my-skill
cat > skills/my-skill/SKILL.md << 'EOF'
---
name: my-skill
description: What this skill does
triggers:
  - keyword one
  - keyword two
---

## Instructions for the LLM

Step by step instructions here.
EOF
```

Hot-reloads on next message — no restart needed.

---

## Project Structure

```
cmd/akb48/          Entry point
adapters/cli/       stdin/stdout CLI
adapters/telegram/  Telegram long-polling + streaming
internal/
  config/           TOML config + defaults
  runtime/          AKB48Runtime, HandleMessage, New()
  llm/              Anthropic SDK wrapper, tool loop
  tools/            Tool registry, idempotency
  session/          JSONL persistence, hooks, cold opener
  identity/         4-tier context assembler
  skills/           Skill resolver (hot-reload)
  brain/            MCP client, GBrain bridge, degraded mode
identity/           AGENTS.md, SOUL.md, USER.md
skills/             note-capture/SKILL.md, research/SKILL.md
deploy/             systemd unit, docker-compose
scripts/            healthcheck.sh
tests/integration/  End-to-end tests (hermetic)
```

---

## Phase Summary

| Phase | Name | Tickets | SP | Status |
|-------|------|---------|----|--------|
| 0 | Foundation | KLW-001–003 | 11 | ✅ Done |
| 1 | Session & Runtime | KLW-004–009 | 27 | ✅ Done |
| 2 | Identity & Skills | KLW-010–014 | 18 | ✅ Done |
| 3 | GBrain Integration | KLW-015–017 | 16 | ✅ Done |
| 4 | Telegram Adapter | KLW-018–019 | 10 | ✅ Done |
| 5 | Hello World Validation | KLW-020–021 | 10 | ✅ Done |

**Total:** 92 story points · 21 tickets · ~2,500 lines of Go

---

## Hello World Definition of Done (PRD-00 §6)

- [x] `./akb48` starts — brain connected, skills loaded, adapters running
- [x] Telegram: "Hello, who are you?" → personality-consistent response
- [x] Telegram: "Remember that staging cluster is ap-southeast-1" → stored in GBrain
- [x] Telegram: "What do you know about our staging cluster?" → retrieved from GBrain
- [x] Kill process, restart → previous session context available
- [ ] Binary runs 24h on VPS without crash ← manual validation on VPS

---

## Key Architecture Decisions (locked)

| Area | Decision |
|------|----------|
| Language | Go 1.25+ |
| LLM primary | `claude-sonnet-4-6-20260326` |
| LLM extraction | `claude-haiku-4-5-20251001` |
| Anthropic SDK | `github.com/anthropics/anthropic-sdk-go` |
| Telegram | `github.com/go-telegram-bot-api/telegram-bot-api/v5` |
| Config | TOML via `github.com/BurntSushi/toml` |
| YAML skills | `gopkg.in/yaml.v3` |
| Session storage | Append-only JSONL, one file per session |
| Brain transport | stdio (JSON-RPC 2.0 over subprocess) |
| Concurrency | goroutines + channels; `sync.RWMutex` for shared state |
| Post-turn hooks | `golang.org/x/sync/errgroup` with 5s deadline |
| Context caching | 4-tier: Identity (cached) → History (cached) → Cold opener → Live turn |

---

## Plan Documents

| File | Content |
|------|---------|
| [phase-1-session-runtime.md](./phase-1-session-runtime.md) | KLW-004 through KLW-009 |
| [phase-2-identity-skills.md](./phase-2-identity-skills.md) | KLW-010 through KLW-014 |
| [phase-3-gbrain-integration.md](./phase-3-gbrain-integration.md) | KLW-015 through KLW-017 |
| [phase-4-telegram-adapter.md](./phase-4-telegram-adapter.md) | KLW-018 through KLW-019 |
| [phase-5-hello-world.md](./phase-5-hello-world.md) | KLW-020 through KLW-021 |
