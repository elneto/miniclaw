# 🐾 MiniClaw

**A poor man's remote agent — control your machines through Telegram + Ollama.**

MiniClaw is a single-binary Go application that turns your Telegram bot into a remote command center for your laptop, Raspberry Pi, or any Linux machine. It connects Telegram → Ollama → Bash and back, giving you an AI-powered remote shell you can use from your phone.

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    YOUR PHONE                            │
│                  (Telegram App)                           │
└──────────────────────┬──────────────────────────────────┘
                       │
                       │ Telegram Bot API (HTTPS)
                       │
┌──────────────────────▼──────────────────────────────────┐
│                  MiniClaw Agent                          │
│            (single Go binary, ~8MB)                      │
│                                                          │
│  ┌──────────────┐  ┌───────────────┐  ┌──────────────┐ │
│  │   Telegram    │  │    Ollama     │  │   Command    │ │
│  │   Handler     │──│    Bridge     │──│   Executor   │ │
│  │              │  │  (REST API)   │  │  (bash -c)   │ │
│  └──────┬───────┘  └───────────────┘  └──────┬───────┘ │
│         │                                      │         │
│  ┌──────▼───────┐                     ┌───────▼──────┐ │
│  │    File      │                     │    Cron      │ │
│  │   Manager    │                     │  Scheduler   │ │
│  │ (workspace/) │                     │ (persistent) │ │
│  └──────────────┘                     └──────────────┘ │
│                                                          │
└──────────────────────┬──────────────────────────────────┘
                       │
             ┌─────────▼─────────┐
             │   Ollama Server   │
             │  (localhost:11434) │
             │                   │
             │  llama3.2 / phi3  │
             │  mistral / etc.   │
             └───────────────────┘
```

## How It Works

### The Communication Loop

```
1. You type on Telegram → "check my disk usage and clean /tmp if above 80%"
2. Telegram API delivers message to MiniClaw
3. MiniClaw wraps your message with a system prompt and sends to Ollama
4. Ollama (running locally, free) generates a response with bash commands:
   "I'll check disk usage. Here's what I'll do:
   ```bash
   usage=$(df / | awk 'NR==2{print $5}' | tr -d '%')
   if [ $usage -gt 80 ]; then
     rm -rf /tmp/*
     echo "Cleaned /tmp. Usage was ${usage}%"
   else
     echo "Disk usage is ${usage}% — all good"
   fi
   ```"
5. MiniClaw extracts the bash block and asks you: "Execute? /yes or /no"
6. You tap /yes
7. MiniClaw runs the command, captures output
8. Result sent back to you on Telegram:
   "✅ Success (0.3s)
    Disk usage is 42% — all good"
9. MiniClaw feeds the result back to Ollama's context for follow-ups
```

### The File Upload Flow (Claude Opus → MiniClaw)

```
1. You ask Claude Opus 4.6 to write a complex script (here, in this chat)
2. Claude generates it, you download the file
3. You upload the .sh/.py file to MiniClaw via Telegram
4. MiniClaw saves it: "💾 Saved: deploy.sh (2.4 KB) — Run with: /run deploy.sh"
5. You type: /run deploy.sh
6. MiniClaw executes it and streams the output back
```

This is the key insight: **use Claude for the hard thinking (free in your subscription), use Ollama for the lightweight routing and simple tasks, and use MiniClaw as the glue.**

---

## Quick Start

### 1. Prerequisites

| Component | Purpose | Install |
|-----------|---------|---------|
| Go 1.21+  | Build MiniClaw | `https://go.dev/dl/` |
| Ollama    | Local LLM | `curl -fsSL https://ollama.ai/install.sh \| sh` |
| Telegram Bot | Interface | Talk to `@BotFather` on Telegram |

### 2. Create Your Telegram Bot

1. Open Telegram, search for **@BotFather**
2. Send `/newbot`, follow prompts
3. Copy the bot token (looks like `123456:ABC-DEF1234...`)
4. Send `/setcommands` to BotFather, select your bot, then paste:
   ```
   exec - Run a bash command
   run - Execute a workspace script
   ask - Ask Ollama without executing
   ls - List workspace files
   cat - View a file
   rm - Delete a file
   status - System health
   cron - Manage cron jobs
   clear - Reset AI memory
   help - Show all commands
   ```

### 3. Get Your Telegram User ID

Message **@userinfobot** on Telegram — it replies with your numeric ID.

### 4. Pull an Ollama Model

```bash
# For Raspberry Pi (4GB RAM):
ollama pull llama3.2:1b

# For Raspberry Pi (8GB RAM):
ollama pull llama3.2:3b

# For laptop with 16GB+ RAM:
ollama pull llama3.1:8b
```

### 5. Build & Configure

```bash
git clone <your-repo-url> miniclaw && cd miniclaw

# Or just copy the files and:
go mod tidy
make build        # builds for your current platform
# OR
make pi64         # cross-compile for Raspberry Pi 64-bit
# OR
make all          # build for everything

# Setup config
mkdir -p ~/.miniclaw/workspace
cp config.yaml ~/.miniclaw/config.yaml
nano ~/.miniclaw/config.yaml   # add your token + user ID
```

### 6. Run

```bash
./miniclaw -config ~/.miniclaw/config.yaml
```

### 7. Auto-Start on Boot (recommended)

```bash
make install   # copies binary to /usr/local/bin
make systemd   # creates systemd service
sudo systemctl enable --now miniclaw
```

---

## Commands Reference

| Command | Description | Example |
|---------|-------------|---------|
| `/exec <cmd>` | Run bash command directly | `/exec docker ps` |
| `/run <file>` | Execute workspace script | `/run backup.sh` |
| `/ask <prompt>` | Ask Ollama (no execution) | `/ask explain crontab syntax` |
| `/ls` | List workspace files | `/ls` |
| `/cat <file>` | View file contents | `/cat deploy.sh` |
| `/rm <file>` | Delete workspace file | `/rm old-script.sh` |
| `/status` | System health report | `/status` |
| `/cron add` | Add scheduled job | `/cron add backup @daily DB Backup \| pg_dump db > bk.sql` |
| `/cron list` | List all cron jobs | `/cron list` |
| `/cron rm <id>` | Remove a cron job | `/cron rm backup` |
| `/clear` | Reset Ollama memory | `/clear` |
| `/yes` | Confirm pending command | `/yes` |
| `/no` | Cancel pending command | `/no` |
| *(any text)* | Chat with Ollama | "restart nginx and check logs" |
| *(file upload)* | Save to workspace | Upload any file |

---

## Recommended Model Choices

| Machine | RAM | Model | Speed | Quality |
|---------|-----|-------|-------|---------|
| Pi 4 (4GB) | 4GB | `llama3.2:1b` | Fast | Basic |
| Pi 4 (8GB) | 8GB | `llama3.2:3b` | Good | Decent |
| Pi 5 (8GB) | 8GB | `phi3:mini` | Good | Good for code |
| Laptop | 16GB | `llama3.1:8b` | Good | Very good |
| Laptop | 32GB | `mistral:7b` | Fast | Excellent |
| Laptop | 32GB | `codellama:13b` | Moderate | Best for code |

---

## The Hybrid Workflow

This is where MiniClaw really shines as a **cost-effective AI agent**:

```
┌──────────────────────────────────────────────────────────────┐
│                     YOUR WORKFLOW                             │
│                                                              │
│  HARD TASKS (complex scripts, analysis, debugging):          │
│  ┌─────────────────────────────────────────┐                │
│  │  Claude Opus 4.6 (via claude.ai)        │                │
│  │  • Write complex deployment scripts     │                │
│  │  • Debug tricky issues                  │                │
│  │  • Generate Dockerfiles, configs        │                │
│  │  • Architect solutions                  │   FREE with    │
│  │  • Download the files                   │   your sub     │
│  └────────────────────┬────────────────────┘                │
│                       │ upload file                          │
│                       ▼                                      │
│  EXECUTION + SIMPLE TASKS:                                   │
│  ┌─────────────────────────────────────────┐                │
│  │  MiniClaw + Ollama (on your machine)    │                │
│  │  • Execute uploaded scripts             │                │
│  │  • Quick sysadmin tasks                 │   FREE         │
│  │  • Monitor services                     │   forever      │
│  │  • Cron jobs                            │                │
│  │  • "restart docker", "check logs"       │                │
│  └─────────────────────────────────────────┘                │
│                                                              │
│  RESULT: Full AI agent capabilities, ~$0/month extra         │
└──────────────────────────────────────────────────────────────┘
```

---

## Security Notes

- **Auth**: Only Telegram user IDs in `allowed_ids` can interact with the bot
- **Confirmation**: By default, AI-suggested commands require `/yes` to execute
- **Timeouts**: Commands are killed after the configured timeout
- **Workspace isolation**: Uploaded files go to a dedicated directory
- **No root**: Run MiniClaw as a regular user, not root
- **Network**: The bot only makes outbound connections (to Telegram API + local Ollama)

⚠️ **MiniClaw gives you remote shell access.** Treat your Telegram bot token like a password. If compromised, revoke it via @BotFather immediately.

---

## Troubleshooting

**Ollama not responding?**
```bash
# Check if Ollama is running
curl http://localhost:11434/api/tags

# Start it
ollama serve &
```

**Bot not receiving messages?**
- Make sure you messaged the bot first (it can't initiate)
- Check your user ID matches `allowed_ids`
- Verify the token with: `curl https://api.telegram.org/bot<TOKEN>/getMe`

**Raspberry Pi too slow?**
- Use a smaller model: `ollama pull llama3.2:1b`
- Or skip Ollama entirely — just use `/exec` for direct commands

---

## License

MIT — do whatever you want with it.
