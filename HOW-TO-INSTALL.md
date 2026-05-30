# HOW-TO-INSTALL

Stand up AKB48 on Docker with Discord in under 15 minutes.

---

## Prerequisites

- Docker + Docker Compose v2
- A [Claude Pro or Max](https://claude.ai) account (for Claude Code CLI auth)
- A Discord account (to create the bot)

---

## Setup

### 1. Clone the repo

```bash
git clone https://github.com/dydanz/akb48.git
cd akb48
```

### 2. Get your credentials

**Claude Code OAuth token** — auth token for the `claude-cli` backend. Run once on any machine with a browser:

```bash
npm install -g @anthropic-ai/claude-code
claude setup-token
```

Copy the token — it looks like `sk-ant-oat01-...`. You will not see it again.

**Discord bot token:**

1. Go to [discord.com/developers/applications](https://discord.com/developers/applications) → **New Application**
2. Name it (e.g. `Kabayan`), then go to **Bot** → **Reset Token** → copy the token
3. Under **Bot**, enable **Message Content Intent** and **Server Members Intent**
4. Go to **OAuth2 → URL Generator**: scope `bot`, permission `Send Messages` + `Read Messages/View Channels`
5. Open the generated URL and invite the bot to your server

**Your Discord user ID:** Settings → Advanced → Developer Mode ON, then right-click your username → **Copy User ID**.

### 3. Create `.env`

At the repo root (never commit this file — it is already in `.gitignore`):

```bash
cat > .env << 'EOF'
CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-...
DISCORD_BOT_TOKEN=your-discord-bot-token
EOF
```

### 4. Authorize your Discord user

Edit `deploy/config.docker.toml` and replace `allowed_user_ids` with your Discord user ID:

```toml
[adapters.discord]
allowed_user_ids = ["YOUR_DISCORD_USER_ID"]
```

Only users in this list can interact with the agent. All others are silently ignored.

### 5. Build and run

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

### 6. Test the agent

In any Discord channel where the bot has access:

```
@Kabayan hello
```

The bot should reply within a few seconds. Test slash command:

```
/ask what can you do?
```

**Test brain persistence** — send a fact, restart, ask it back:

```
@Kabayan remember that our main database is PostgreSQL on port 5432
```

```bash
docker compose restart akb48
```

```
@Kabayan what database are we using?
```

The agent should recall the fact from its persistent brain (stored in the `brain` Docker volume at `/app/brain/memory.jsonl`).

---

## Configuration

### Reference

| File | Purpose |
|------|---------|
| `.env` | Secrets — never commit |
| `deploy/config.docker.toml` | Runtime config — model, adapters, brain, session |
| `identity/AGENTS.md` | Operational rules injected into every prompt |
| `identity/SOUL.md` | Personality and tone |
| `identity/USER.md` | Operator profile |
| `skills/*/SKILL.md` | Domain skills — add files, no code changes needed |

### Switching backends

The backend is read from `deploy/config.docker.toml` at startup. Changing it requires only a container restart — no rebuild, no down/up. **Brain memory and session history are stored in Docker volumes and are unaffected by backend switches.**

**claude-cli → Anthropic API key:**

1. Edit `deploy/config.docker.toml`:

```toml
[llm]
backend = "api"
api_key_env = "ANTHROPIC_API_KEY"
```

2. Add the key to `.env`:

```bash
echo "ANTHROPIC_API_KEY=sk-ant-api03-..." >> .env
```

3. Restart: `docker compose restart akb48`

**Anthropic API key → claude-cli:**

1. Edit `deploy/config.docker.toml`:

```toml
[llm]
backend = "claude-cli"
# remove or comment out api_key_env
```

2. Ensure `CLAUDE_CODE_OAUTH_TOKEN` is set in `.env`.

3. Restart: `docker compose restart akb48`

---

## Brain & Memory

### Inspecting the knowledge graph

There is no GUI. `mcp-server-memory` stores all entities as a flat JSONL file — one JSON object per line.

```bash
# Raw dump
docker exec deploy-akb48-1 cat /app/brain/memory.jsonl

# Pretty-print (requires jq on host)
docker exec deploy-akb48-1 cat /app/brain/memory.jsonl | jq .

# List all entity names and types
docker exec deploy-akb48-1 cat /app/brain/memory.jsonl \
  | jq -r 'select(.type == "entity") | "\(.name) (\(.entityType))"'

# Search for a keyword
docker exec deploy-akb48-1 cat /app/brain/memory.jsonl \
  | jq -r 'select(.type == "entity") | select(.name | ascii_downcase | contains("YOUR_KEYWORD"))'
```

**Interactive browser via MCP Inspector** — web UI to call tools directly (search, create, delete entities):

```bash
# Get the volume path on host
docker volume inspect deploy_brain | jq -r '.[0].Mountpoint'

# Run inspector (replace path with output above)
MEMORY_FILE_PATH=/var/lib/docker/volumes/deploy_brain/_data/memory.jsonl \
  npx @modelcontextprotocol/inspector mcp-server-memory
```

Open `http://localhost:5173` to call `search_nodes`, `read_graph`, `create_entities` interactively.

**Ask the agent directly:**

```
@Kabayan what do you know about our infrastructure?
@Kabayan search your brain for all decisions we made
```

### Resetting memory

**Wipe facts only** (mcp-server-memory reloads from file on each call — no restart needed):

```bash
docker exec deploy-akb48-1 sh -c "truncate -s 0 /app/brain/memory.jsonl"
```

**Wipe and restart** (cleanest):

```bash
docker exec deploy-akb48-1 sh -c "truncate -s 0 /app/brain/memory.jsonl" && docker compose restart akb48
```

**Full volume wipe** (irreversible):

```bash
docker compose down
docker volume rm deploy_brain
docker compose up -d akb48
```

Session history (conversation turns) is separate — stored in the `sessions` volume:

```bash
docker exec deploy-akb48-1 sh -c "rm -f /app/sessions/*.jsonl" && docker compose restart akb48
```

---

## Adding skills

Skills teach the agent new behaviors without touching Go code. Drop a file, the agent picks it up on the next message.

### Skill file format

Create a directory under `skills/` with a `SKILL.md`:

```
skills/
└── my-skill/
    └── SKILL.md
```

```markdown
---
name: my-skill
description: >
  One-line description of when this skill applies.
triggers:
  - keyword one
  - keyword two
  - "longer trigger phrase"
---

## What to do

Step-by-step instructions the agent follows when this skill is active.
Include: input expectations, output format, constraints, examples.
Keep under 2,000 tokens.
```

On every message the resolver lowercases the text and checks each skill's `triggers` for a substring match. The most specific match (longest trigger string) wins. Only one skill is injected per turn. No match → general mode.

### Deploy to Docker

Skills are baked into the image at build time. After adding or editing a skill:

```bash
cd deploy
docker compose build akb48
docker compose up -d akb48
```

Verify it loaded:

```bash
docker exec deploy-akb48-1 ls /app/skills/
```

Test by sending a message in Discord containing one of your trigger phrases.

### Existing skills

| Skill | Triggers (examples) |
|-------|---------------------|
| `note-capture` | "remember", "store this", "add to brain" |
| `research` | "research", "compare", "what are the options for" |

---

## Operations

### Useful commands

```bash
# View live logs
docker compose logs -f akb48

# Confirm backend mode
docker exec deploy-akb48-1 grep "backend" /app/config.toml

# Stop
docker compose down

# Restart without rebuild
docker compose restart akb48
```

### Docker shell

Access the running container:

```bash
docker exec -it deploy-akb48-1 sh
```

**What you can do inside:**

```sh
cat /app/config.toml              # config in use
cat /app/brain/memory.jsonl       # all stored facts
ls /app/skills/                   # loaded skills
ls /app/identity/                 # identity files
ls /app/sessions/                 # session files
which mcp-server-memory           # verify brain binary present
```

**What you cannot do inside:**

- Run `claude` interactively — requires TTY + browser auth flow not available in container
- Edit files — container filesystem is read-only except volume-mounted paths (`/app/sessions`, `/app/logs`, `/app/brain`)
- Persist config changes — `config.toml` is bind-mounted read-only from `deploy/config.docker.toml`; edit the host file and restart

Exit with `exit` or `Ctrl+D`.

---

## Troubleshooting

**Bot not responding** — check `allowed_user_ids` in `deploy/config.docker.toml` contains your Discord user ID.

**`brain=disabled` in logs** — `brain.enabled` is `false` in config, or `mcp-server-memory` failed to start. Check `docker compose logs akb48`.

**`/app/brain` permission denied** — `docker compose build akb48 --no-cache && docker compose up -d akb48`.

**`claude: command not found`** — `docker compose build akb48 --no-cache` to get a fresh npm install.

**OAuth token expired** — re-run `claude setup-token`, update `.env`, then `docker compose restart akb48`.
