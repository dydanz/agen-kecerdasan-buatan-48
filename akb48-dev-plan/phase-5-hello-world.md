# Phase 5: Hello World Validation & Deployment

**Goal:** Prove the full end-to-end pipeline works — chat to LLM to brain to persistence — and deploy to a VPS so it runs 24/7 without the operator's laptop.

**Definition of Done (PRD-00 §6):**
1. `./akb48` starts — brain connected, skills loaded, adapters running
2. Telegram: "Hello, who are you?" → personality-consistent response
3. Telegram: "Remember that staging cluster is ap-southeast-1" → stored in GBrain
4. Telegram: "What do you know about our staging cluster?" → retrieved from GBrain
5. Kill process, restart → previous session context available
6. Binary runs 24h on VPS without crash

**Tickets:** KLW-020 → KLW-021

---

## KLW-020 — End-to-End Integration Tests

**Type:** Chore
**Owner:** Backend
**Effort:** 5 SP
**Labels:** `phase/5`, `type/chore`, `size/M`, `component/runtime`
**Dependencies:** All Phase 1–4 tickets
**Branch:** `feat/integration-tests`

### Description

Automated integration tests that validate the full Hello World flow. Tests are hermetic (no real API calls, no real GBrain) — they use mocks and a temp directory for session storage.

### Implementation Plan

**Files to create:**
- `tests/integration/hello_world_test.go`
- `tests/integration/helpers_test.go`

**Test helpers:**

```go
// helpers_test.go
package integration_test

import (
    "context"
    "testing"
    "os"

    "github.com/dydanz/akb48/internal/config"
    "github.com/dydanz/akb48/internal/runtime"
)

// buildTestRuntime creates a fully-wired runtime with:
// - Real session manager (temp dir storage)
// - Real skill resolver (from skills/ dir)
// - Real context assembler (from identity/ dir)
// - Mock LLM caller (returns scripted responses)
// - Mock GBrain bridge (records calls, returns scripted results)
func buildTestRuntime(t *testing.T, opts ...testOption) *runtime.AKB48Runtime

type mockLLMCaller struct {
    responses []string
    calls     []llm.CallParams
    idx       int
}

type mockBrainBridge struct {
    putCalls    []json.RawMessage
    searchCalls []string
    searchResult string
}
```

**Test 1 — CLI multi-turn with session persistence:**

```go
func TestHelloWorld_MultiTurnPersistence(t *testing.T) {
    tmpDir := t.TempDir()

    // Round 1: two turns
    rt1 := buildTestRuntime(t, withStorageDir(tmpDir))
    rt1.Start(ctx)

    sendMessage(rt1, "main:cli:local", "Hello")
    sendMessage(rt1, "main:cli:local", "What is 2+2?")
    rt1.Stop()

    // Round 2: new runtime, same storage dir
    rt2 := buildTestRuntime(t, withStorageDir(tmpDir))
    rt2.Start(ctx)

    sess, _ := rt2.SessionManager.ResolveOrCreate(ctx, "main:cli:local")
    assert sess.TurnCount() == 2, "expected 2 turns from previous runtime"
}
```

**Test 2 — Memory roundtrip (note-capture → brain → recall):**

```go
func TestHelloWorld_MemoryRoundtrip(t *testing.T) {
    bridge := &mockBrainBridge{}
    rt := buildTestRuntime(t, withBrainBridge(bridge))

    // Turn 1: store a fact
    sendMessage(rt, "main:telegram:123", "remember that staging cluster is ap-southeast-1")

    // Assert gbrain_put was called
    assert len(bridge.putCalls) == 1
    var entity map[string]interface{}
    json.Unmarshal(bridge.putCalls[0], &entity)
    assert strings.Contains(entity["title"].(string), "staging")

    // Turn 2: retrieve the fact
    bridge.searchResult = `- Staging cluster is ap-southeast-1 (decision, 2026-05-01)`
    sendMessage(rt, "main:telegram:123", "what do you know about staging?")

    // Assert gbrain_search was called
    assert len(bridge.searchCalls) >= 1
}
```

**Test 3 — Skill routing:**

```go
func TestHelloWorld_SkillRouting(t *testing.T) {
    var capturedSystem string
    mockLLM := &mockLLMCaller{
        onCall: func(params llm.CallParams) {
            capturedSystem = params.System
        },
        responses: []string{"Stored."},
    }
    rt := buildTestRuntime(t, withLLMCaller(mockLLM))

    // "remember" → note-capture skill injected
    sendMessage(rt, "main:cli:local", "remember that X")
    assert strings.Contains(capturedSystem, "note-capture") ||
           strings.Contains(capturedSystem, "gbrain_put"),
           "note-capture skill body should appear in system prompt"

    // "hello" → no skill
    capturedSystem = ""
    sendMessage(rt, "main:cli:local", "hello")
    assert !strings.Contains(capturedSystem, "note-capture")
}
```

**Test 4 — Brain degraded mode:**

```go
func TestHelloWorld_BrainDegraded(t *testing.T) {
    // Brain bridge is nil (disabled)
    rt := buildTestRuntime(t, withNoBrain())

    // Should still respond
    response := sendMessage(rt, "main:cli:local", "hello")
    assert response != "", "agent should respond even with no brain"
    // No panic, no error
}
```

**Test 5 — Cold session opener:**

```go
func TestHelloWorld_ColdOpener(t *testing.T) {
    bridge := &mockBrainBridge{
        searchResult: "- Staging cluster: ap-southeast-1 (decision)",
    }
    var capturedMessages []anthropic.MessageParam
    mockLLM := &mockLLMCaller{
        onCall: func(params llm.CallParams) {
            capturedMessages = params.Messages // capture for assertion
        },
    }
    rt := buildTestRuntime(t, withBrainBridge(bridge), withLLMCaller(mockLLM))

    // First message → cold opener should fire
    sendMessage(rt, "main:cli:local", "what is the staging cluster region?")

    // Cold context should appear as early message in the slice
    found := false
    for _, msg := range capturedMessages {
        if strings.Contains(fmt.Sprint(msg), "What I recall") {
            found = true
        }
    }
    assert found, "cold context should be injected in first message of session"
}
```

**Test 6 — Post-turn hooks:**

```go
func TestHelloWorld_PostTurnHooks(t *testing.T) {
    tmpDir := t.TempDir()
    rt := buildTestRuntime(t, withStorageDir(tmpDir))

    sendMessage(rt, "main:cli:local", "hello")

    // Assert session JSONL was written
    path := filepath.Join(tmpDir, "main_cli_local.jsonl")
    data, _ := os.ReadFile(path)
    assert strings.Contains(string(data), "hello"), "session turn should be persisted"

    // Assert metrics log was written
    metricsPath := filepath.Join(tmpDir, "tool-calls.jsonl")
    _, err := os.Stat(metricsPath)
    assert err == nil, "tool-calls.jsonl should exist after a turn"
}
```

### Acceptance Criteria

- [ ] All 6 integration tests pass
- [ ] Tests complete in under 30s: `go test ./tests/integration/... -timeout 60s`
- [ ] Tests are hermetic — no network calls, no real API, no real GBrain
- [ ] Tests use `t.TempDir()` — no test state on disk after run

### Testing Plan

```bash
go test ./tests/integration/... -v -timeout 60s
# Expected output:
# --- PASS: TestHelloWorld_MultiTurnPersistence (0.12s)
# --- PASS: TestHelloWorld_MemoryRoundtrip (0.05s)
# --- PASS: TestHelloWorld_SkillRouting (0.04s)
# --- PASS: TestHelloWorld_BrainDegraded (0.03s)
# --- PASS: TestHelloWorld_ColdOpener (0.06s)
# --- PASS: TestHelloWorld_PostTurnHooks (0.08s)
# PASS
```

---

## KLW-021 — VPS Deployment & Operations Setup

**Type:** Chore
**Owner:** DevOps / Backend
**Effort:** 5 SP
**Labels:** `phase/5`, `type/chore`, `size/M`, `component/runtime`
**Dependencies:** KLW-019 (Telegram adapter), KLW-020 (tests passing)
**Branch:** `feat/deployment`

### Description

Deploy AKB48 to a Hetzner VPS with GBrain in Docker, systemd service for the Go binary, environment secrets in a restricted `.env` file, and a `Makefile` for repeatable deploys.

### Implementation Plan

**Files to create:**
- `Makefile`
- `docker-compose.yml` (GBrain + PostgreSQL)
- `deplo./akb48.service` (systemd unit)
- `.env.example`
- `scripts/healthcheck.sh`
- `scripts/deploy.sh`

**docker-compose.yml — GBrain + PostgreSQL:**

```yaml
version: "3.9"

services:
  postgres:
    image: pgvector/pgvector:pg16
    restart: unless-stopped
    environment:
      POSTGRES_DB: brain
      POSTGRES_USER: brain
      POSTGRES_PASSWORD_FILE: /run/secrets/pg_password
    volumes:
      - pgdata:/var/lib/postgresql/data
    secrets:
      - pg_password

  gbrain:
    image: ghcr.io/gbrain/gbrain:latest
    restart: unless-stopped
    depends_on:
      - postgres
    environment:
      DATABASE_URL: postgres://brain@postgres/brain
    volumes:
      - ~/brain:/brain
    command: ["serve", "--dir", "/brain"]

volumes:
  pgdata:

secrets:
  pg_password:
    file: ./secrets/pg_password
```

**deplo./akb48.service — systemd unit:**

```ini
[Unit]
Description=AKB48 AI Agent Runtime
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=dandi
WorkingDirectory=/home/dand./akb48
EnvironmentFile=/home/dandi/.env
ExecStartPre=/home/dand./akb48/akb48 --validate
ExecStart=/home/dand./akb48/akb48
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=akb48

[Install]
WantedBy=multi-user.target
```

**Makefile:**

```makefile
BINARY=akb48
VPS_USER=dandi
VPS_HOST=<VPS_IP>
VPS_DIR=/home/dand./akb48

.PHONY: build test deploy logs restart validate

build:
	go build -o $(BINARY) ./cmd/akb48/

test:
	go test ./... -timeout 120s

validate: build
	ANTHROPIC_API_KEY=test ./$(BINARY) --validate

deploy: build test
	rsync -avz $(BINARY) config.toml identity/ skills/ $(VPS_USER)@$(VPS_HOST):$(VPS_DIR)/
	ssh $(VPS_USER)@$(VPS_HOST) "sudo systemctl restart akb48"
	@echo "Deployed. Check: ssh $(VPS_USER)@$(VPS_HOST) 'journalctl -u akb48 -f'"

logs:
	ssh $(VPS_USER)@$(VPS_HOST) "journalctl -u akb48 -f --no-pager"

restart:
	ssh $(VPS_USER)@$(VPS_HOST) "sudo systemctl restart akb48"

setup-vps:
	ssh $(VPS_USER)@$(VPS_HOST) "mkdir -p $(VPS_DIR)/sessions $(VPS_DIR)/logs"
	rsync -avz deplo./akb48.service $(VPS_USER)@$(VPS_HOST):/etc/systemd/system/
	ssh $(VPS_USER)@$(VPS_HOST) "sudo systemctl daemon-reload && sudo systemctl enable akb48"
```

**.env.example:**

```bash
# AKB48 environment variables
# Copy to .env and fill in values. Never commit .env.
ANTHROPIC_API_KEY=sk-ant-...
TELEGRAM_BOT_TOKEN=...
```

**scripts/healthcheck.sh:**

```bash
#!/bin/bash
# Check that GBrain is responding
docker exec akb48_gbrain_1 gbrain status 2>/dev/null
if [ $? -ne 0 ]; then
    echo "GBrain is not responding"
    exit 1
fi

# Check that akb48 systemd service is active
systemctl is-active --quiet akb48
if [ $? -ne 0 ]; then
    echo "akb48 service is not active"
    exit 1
fi

echo "All systems operational"
exit 0
```

### Acceptance Criteria

- [ ] `make deploy` builds, runs tests, rsyncs to VPS, restarts service
- [ ] Deploy fails if `go test ./...` fails (tests are a deploy gate)
- [ ] `ExecStartPre=./akb48 --validate` in systemd — deploy aborts if config invalid
- [ ] `systemctl status akb48` shows `active (running)` after deploy
- [ ] GBrain container starts automatically on VPS boot
- [ ] `journalctl -u akb48 -f` shows structured slog output
- [ ] Process killed by systemd → restarts within 5s (`RestartSec=5`)
- [ ] **Hello World end-to-end manual test passes** (PRD-00 §6, all 6 steps)
- [ ] Binary runs 24h without crash on VPS

### Testing Plan

**Deploy verification checklist:**
```bash
# 1. Deploy
make deploy

# 2. Check service
ssh dandi@VPS "systemctl status akb48"
# Expected: active (running)

# 3. Check logs
ssh dandi@VPS "journalctl -u akb48 --since '1 min ago'"
# Expected: "AKB48 started adapters=CLI,Telegram brain=connected"

# 4. Send Telegram message
# "Hello, who are you?"
# Expected: personality-consistent response (no emojis, direct)

# 5. Memory roundtrip
# "Remember that staging cluster is ap-southeast-1"
# Expected: "Stored. Staging cluster region: ap-southeast-1"
# "What do you know about our staging cluster?"
# Expected: references ap-southeast-1

# 6. Restart resilience
ssh dandi@VPS "sudo systemctl restart akb48"
# Wait 5 seconds
# "What did I just ask you to remember?"
# Expected: recalls staging cluster from loaded session

# 7. 24h uptime
# Check after 24h: journalctl --since yesterday | grep -c "INFO"
# Expected: > 0 (process stayed alive)
```

**Health check:**
```bash
scripts/healthcheck.sh
# Expected: "All systems operational"
```
