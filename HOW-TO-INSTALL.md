# HOW-TO-INSTALL

Stand up AKB48 on Docker with Discord in under 15 minutes.

---

## Prerequisites

- Docker + Docker Compose v2
- A [Claude Pro or Max](https://claude.ai) account (for Claude Code CLI auth)
- A Discord account (to create the bot)

---

## 1. Clone the repo

```bash
git clone https://github.com/dydanz/akb48.git
cd akb48
```

---

## 2. Get your credentials

### Claude Code OAuth token

This is the auth token for the `claude-cli` backend. Run it once on any machine with a browser:

```bash
npm install -g @anthropic-ai/claude-code
claude setup-token
```

Copy the token — it looks like `sk-ant-oat01-...`. You will not see it again.

### Discord bot token

1. Go to [discord.com/developers/applications](https://discord.com/developers/applications) → **New Application**
2. Name it (e.g. `Kabayan`), then go to **Bot** → **Reset Token** → copy the token
3. Under **Bot**, enable **Message Content Intent** and **Server Members Intent**
4. Go to **OAuth2 → URL Generator**: scope `bot`, permission `Send Messages` + `Read Messages/View Channels`
5. Open the generated URL and invite the bot to your server

### Your Discord user ID

In Discord: **Settings → Advanced → Developer Mode ON**, then right-click your username → **Copy User ID**.

---

## 3. Create `.env`

At the repo root (never commit this file — it is already in `.gitignore`):

```bash
cat > .env << 'EOF'
CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-...
DISCORD_BOT_TOKEN=your-discord-bot-token
EOF
```

---

## 4. Authorize your Discord user

Edit `deploy/config.docker.toml` and replace the `allowed_user_ids` with your own Discord user ID:

```toml
[adapters.discord]
allowed_user_ids = ["YOUR_DISCORD_USER_ID"]
```

Only users in this list can interact with the agent. All others are silently ignored.

---

## 5. Build and run

```bash
cd deploy
docker compose build akb48
docker compose up -d akb48
```

The `postgres` and `gbrain` services in `docker-compose.yml` are not required — AKB48 uses `mcp-server-memory` (bundled in the image) as its brain backend.

Verify startup:

```bash
docker compose logs -f akb48
```

Expected output:

```
INFO GBrain connected tools=9
INFO AKB48 initialised brain="connected (9 tools)" skills=2
INFO AKB48 started model=claude-sonnet-4-6
INFO Discord adapter started username=Kabayan
```

---

## 6. Test the agent

In any Discord channel where the bot has access, type:

```
@Kabayan hello
```

The bot should reply within a few seconds. If it doesn't respond, check `docker compose logs akb48`.

### Test slash command

```
/ask what can you do?
```

### Test brain persistence

Send a fact:

```
@Kabayan remember that our main database is PostgreSQL on port 5432
```

Restart the container:

```bash
docker compose restart akb48
```

Ask it back:

```
@Kabayan what database are we using?
```

The agent should recall the fact from its persistent brain (stored in the `brain` Docker volume at `/app/brain/memory.jsonl`).

---

## Useful commands

```bash
# View live logs
docker compose logs -f akb48

# Confirm backend mode
docker exec deploy-akb48-1 grep "backend" /app/config.toml

# Inspect brain storage
docker exec deploy-akb48-1 cat /app/brain/memory.jsonl

# Stop
docker compose down
```

---

## Configuration reference

| File | Purpose |
|------|---------|
| `.env` | Secrets (never commit) |
| `deploy/config.docker.toml` | Runtime config — model, adapters, brain, session |
| `identity/AGENTS.md` | Operational rules injected into every prompt |
| `identity/SOUL.md` | Personality and tone |
| `identity/USER.md` | Operator profile |
| `skills/*/SKILL.md` | Domain skills — add files, no code changes needed |

---

## Troubleshooting

**Bot not responding** — check `allowed_user_ids` in `deploy/config.docker.toml` contains your Discord user ID.

**`brain=disabled` in logs** — `brain.enabled` is `false` in config, or `mcp-server-memory` failed to start. Check `docker compose logs akb48` for the error.

**`/app/brain` permission denied** — rebuild the image: `docker compose build akb48 --no-cache`, then `docker compose up -d akb48`.

**`claude: command not found`** — the image installs `@anthropic-ai/claude-code` globally. If this fails, `docker compose build akb48 --no-cache` to get a fresh npm install.

**OAuth token expired** — re-run `claude setup-token`, update `.env`, then `docker compose up -d akb48`.
