package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api         *tgbotapi.BotAPI
	config      *Config
	ollama      *OllamaClient
	executor    *Executor
	scheduler   *Scheduler
	allowedIDs  map[int64]bool
	pendingCmds map[int64]string // commands waiting for /yes confirmation
	startTime   time.Time
}

func NewBot(cfg *Config, ollama *OllamaClient, executor *Executor) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.Telegram.Token)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	allowed := make(map[int64]bool)
	for _, id := range cfg.Telegram.AllowedIDs {
		allowed[id] = true
	}

	bot := &Bot{
		api:         api,
		config:      cfg,
		ollama:      ollama,
		executor:    executor,
		allowedIDs:  allowed,
		pendingCmds: make(map[int64]string),
		startTime:   time.Now(),
	}

	// Create scheduler with Telegram notification callback
	bot.scheduler = NewScheduler(cfg.Scheduler, executor, func(msg string) {
		for id := range allowed {
			bot.sendMessage(id, msg)
		}
	})

	return bot, nil
}

func (b *Bot) Start() error {
	b.scheduler.Start()
	defer b.scheduler.Stop()

	log.Printf("🐾 MiniClaw online as @%s", b.api.Self.UserName)
	log.Printf("   Ollama: %s (%s)", b.config.Ollama.URL, b.config.Ollama.Model)
	log.Printf("   Workspace: %s", b.config.Executor.Workspace)
	log.Printf("   Allowed users: %v", b.config.Telegram.AllowedIDs)

	// Notify all allowed users that we're online
	for id := range b.allowedIDs {
		b.sendMessage(id, fmt.Sprintf("🐾 MiniClaw is online!\nHost: %s (%s)\nModel: %s\nSend /help for commands.",
			hostname(), runtime.GOARCH, b.config.Ollama.Model))
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}
		go b.handleMessage(update.Message)
	}

	return nil
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	// Auth check
	if !b.allowedIDs[msg.From.ID] {
		b.reply(msg, "⛔ Unauthorized. Your ID: `"+fmt.Sprint(msg.From.ID)+"`\nAdd this to `allowed_ids` in config.yaml")
		return
	}

	// Handle file uploads
	if msg.Document != nil {
		b.handleFileUpload(msg)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Route commands
	switch {
	case text == "/start" || text == "/help":
		b.handleHelp(msg)
	case text == "/status":
		b.handleStatus(msg)
	case strings.HasPrefix(text, "/exec "):
		b.handleExec(msg, strings.TrimPrefix(text, "/exec "))
	case strings.HasPrefix(text, "/run "):
		b.handleRunScript(msg, strings.TrimPrefix(text, "/run "))
	case text == "/ls":
		b.handleListFiles(msg)
	case strings.HasPrefix(text, "/cat "):
		b.handleCatFile(msg, strings.TrimPrefix(text, "/cat "))
	case strings.HasPrefix(text, "/rm "):
		b.handleDeleteFile(msg, strings.TrimPrefix(text, "/rm "))
	case strings.HasPrefix(text, "/download "):
		b.handleDownload(msg, strings.TrimPrefix(text, "/download "))
	case strings.HasPrefix(text, "/ask "):
		b.handleAsk(msg, strings.TrimPrefix(text, "/ask "))
	case text == "/clear":
		b.ollama.ClearHistory()
		b.reply(msg, "🧹 Conversation history cleared.")
	case text == "/yes":
		b.handleConfirm(msg)
	case text == "/no":
		delete(b.pendingCmds, msg.From.ID)
		b.reply(msg, "↩️ Cancelled.")
	case strings.HasPrefix(text, "/cron"):
		b.handleCron(msg, strings.TrimPrefix(text, "/cron"))
	default:
		// Natural language → Ollama
		b.handleChat(msg, text)
	}
}

func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	help := `🐾 *MiniClaw — Remote Command Center*

*Direct Commands:*
/exec <cmd> — Run a bash command directly
/run <file> — Execute a script from workspace
/ls — List workspace files
/cat <file> — View file contents
/rm <file> — Delete a file
/download <file> — Download file from workspace
/status — System health report

*AI Assistant:*
/ask <prompt> — Ask Ollama (won't auto-execute)
Just type naturally — Ollama responds and suggests commands
/clear — Reset conversation memory

*Cron Jobs:*
/cron add <id> <spec> <label> | <command>
/cron list
/cron rm <id>

*File Management:*
Send any file → auto-saved to workspace
Upload same filename → replaces existing file
/download <file> — get file sent back to you
Then use /run <filename> to execute it

*Safety:*
Commands from Ollama need /yes to execute
Direct /exec runs immediately — be careful!

*Examples:*
• /exec df -h
• /exec docker ps
• /ask check disk usage and clean if above 80%
• /cron add backup @daily Backup DB | pg_dump mydb > backup.sql
• Upload a .sh file → /run myscript.sh`

	b.reply(msg, help)
}

func (b *Bot) handleStatus(msg *tgbotapi.Message) {
	result, err := b.executor.Run(`
echo "🖥 $(hostname) ($(uname -m))"
echo "⏱ Uptime: $(uptime -p 2>/dev/null || uptime)"
echo "💾 Memory: $(free -h 2>/dev/null | awk '/^Mem:/{print $3"/"$2}' || vm_stat 2>/dev/null | head -5)"
echo "💿 Disk: $(df -h / | awk 'NR==2{print $3"/"$2" ("$5" used)"}')"
echo "🔥 Load: $(cat /proc/loadavg 2>/dev/null | awk '{print $1,$2,$3}' || sysctl -n vm.loadavg 2>/dev/null)"
echo "🐳 Docker: $(docker ps --format '{{.Names}}' 2>/dev/null | wc -l) containers running"
`)

	uptime := time.Since(b.startTime).Truncate(time.Second)

	status := fmt.Sprintf("📊 *System Status*\n\n")
	if err != nil {
		status += fmt.Sprintf("Error gathering stats: %s\n", err)
	} else {
		status += result.Stdout
	}
	status += fmt.Sprintf("\n🐾 MiniClaw uptime: %s", uptime)
	status += fmt.Sprintf("\n🧠 Model: %s", b.config.Ollama.Model)

	// Check Ollama health
	if err := b.ollama.Ping(); err != nil {
		status += fmt.Sprintf("\n⚠️ Ollama: %s", err)
	} else {
		status += "\n✅ Ollama: connected"
	}

	b.reply(msg, status)
}

func (b *Bot) handleExec(msg *tgbotapi.Message, command string) {
	b.sendMessage(msg.Chat.ID, fmt.Sprintf("⚡ Executing:\n```bash\n%s\n```", command))

	result, err := b.executor.Run(command)
	if err != nil {
		b.reply(msg, "❌ Error: "+err.Error())
		return
	}

	b.reply(msg, FormatResult(result))
}

func (b *Bot) handleRunScript(msg *tgbotapi.Message, args string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		b.reply(msg, "Usage: /run <filename> [args...]")
		return
	}

	filename := parts[0]
	scriptArgs := parts[1:]

	b.sendMessage(msg.Chat.ID, fmt.Sprintf("▶️ Running: `%s`", filename))

	result, err := b.executor.RunScript(filename, scriptArgs...)
	if err != nil {
		b.reply(msg, "❌ "+err.Error())
		return
	}

	b.reply(msg, FormatResult(result))
}

func (b *Bot) handleListFiles(msg *tgbotapi.Message) {
	files, err := b.executor.ListFiles()
	if err != nil {
		b.reply(msg, "❌ "+err.Error())
		return
	}

	if len(files) == 0 {
		b.reply(msg, "📂 Workspace is empty.")
		return
	}

	var sb strings.Builder
	sb.WriteString("📂 *Workspace:*\n\n")
	for _, f := range files {
		icon := "📄"
		if f.IsDir {
			icon = "📁"
		}
		size := formatSize(f.Size)
		sb.WriteString(fmt.Sprintf("%s `%s` (%s, %s)\n", icon, f.Name, size, f.ModTime.Format("Jan 02 15:04")))
	}
	b.reply(msg, sb.String())
}

func (b *Bot) handleCatFile(msg *tgbotapi.Message, filename string) {
	filename = strings.TrimSpace(filename)
	content, err := b.executor.ReadFile(filename)
	if err != nil {
		b.reply(msg, "❌ "+err.Error())
		return
	}
	b.reply(msg, fmt.Sprintf("📄 *%s:*\n```\n%s\n```", filename, content))
}

func (b *Bot) handleDeleteFile(msg *tgbotapi.Message, filename string) {
	filename = strings.TrimSpace(filename)
	if err := b.executor.DeleteFile(filename); err != nil {
		b.reply(msg, "❌ "+err.Error())
		return
	}
	b.reply(msg, fmt.Sprintf("🗑 Deleted: `%s`", filename))
}

func (b *Bot) handleDownload(msg *tgbotapi.Message, filename string) {
	filename = strings.TrimSpace(filepath.Base(filename))
	path := filepath.Join(b.config.Executor.Workspace, filename)

	if _, err := os.Stat(path); err != nil {
		b.reply(msg, "❌ File not found: `"+filename+"`")
		return
	}

	doc := tgbotapi.NewDocument(msg.Chat.ID, tgbotapi.FilePath(path))
	doc.Caption = fmt.Sprintf("📥 %s", filename)
	if _, err := b.api.Send(doc); err != nil {
		b.reply(msg, "❌ Error sending file: "+err.Error())
	}
}

func (b *Bot) handleFileUpload(msg *tgbotapi.Message) {
	doc := msg.Document
	fileConfig := tgbotapi.FileConfig{FileID: doc.FileID}
	file, err := b.api.GetFile(fileConfig)
	if err != nil {
		b.reply(msg, "❌ Error getting file info: "+err.Error())
		return
	}

	// Download the file
	fileURL := file.Link(b.api.Token)
	resp, err := http.Get(fileURL)
	if err != nil {
		b.reply(msg, "❌ Error downloading file: "+err.Error())
		return
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		b.reply(msg, "❌ Error reading file: "+err.Error())
		return
	}

	path, err := b.executor.SaveFile(doc.FileName, data)
	if err != nil {
		b.reply(msg, "❌ Error saving file: "+err.Error())
		return
	}

	b.reply(msg, fmt.Sprintf("💾 Saved: `%s` (%s)\n\nRun with: `/run %s`\nDownload: `/download %s`",
		doc.FileName, formatSize(int64(len(data))), doc.FileName, doc.FileName))
	_ = path
}

func (b *Bot) handleAsk(msg *tgbotapi.Message, prompt string) {
	b.sendMessage(msg.Chat.ID, "🧠 Thinking...")

	response, err := b.ollama.Chat(prompt)
	if err != nil {
		b.reply(msg, "❌ Ollama error: "+err.Error())
		return
	}

	b.reply(msg, response)

	// If response contains commands but /ask was used, don't offer execution
}

func (b *Bot) handleChat(msg *tgbotapi.Message, text string) {
	b.sendMessage(msg.Chat.ID, "🧠 Thinking...")

	response, err := b.ollama.Chat(text)
	if err != nil {
		b.reply(msg, "❌ Ollama error: "+err.Error())
		return
	}

	// Extract bash commands from response
	commands := ExtractBashCommands(response)

	// Send the response
	b.reply(msg, response)

	if len(commands) > 0 {
		combined := strings.Join(commands, "\n")

		if b.config.Ollama.AutoExecute {
			// Auto-execute mode — run immediately
			b.sendMessage(msg.Chat.ID, "⚡ Auto-executing...")
			result, err := b.executor.Run(combined)
			if err != nil {
				b.sendMessage(msg.Chat.ID, "❌ Error: "+err.Error())
			} else {
				b.sendMessage(msg.Chat.ID, FormatResult(result))
			}
		} else {
			// Safe mode — ask for confirmation
			b.pendingCmds[msg.From.ID] = combined
			b.sendMessage(msg.Chat.ID, fmt.Sprintf(
				"🔐 Execute these commands?\n```bash\n%s\n```\n\n/yes to run · /no to cancel",
				combined))
		}
	}
}

func (b *Bot) handleConfirm(msg *tgbotapi.Message) {
	cmd, exists := b.pendingCmds[msg.From.ID]
	if !exists {
		b.reply(msg, "Nothing pending to execute.")
		return
	}

	delete(b.pendingCmds, msg.From.ID)
	b.sendMessage(msg.Chat.ID, "⚡ Executing...")

	result, err := b.executor.Run(cmd)
	if err != nil {
		b.reply(msg, "❌ Error: "+err.Error())
		return
	}

	b.reply(msg, FormatResult(result))

	// Feed the result back to Ollama so it knows what happened
	b.ollama.Chat(fmt.Sprintf("The command was executed. Here is the result:\n\nExit code: %d\nStdout:\n%s\nStderr:\n%s",
		result.ExitCode, result.Stdout, result.Stderr))
}

func (b *Bot) handleCron(msg *tgbotapi.Message, args string) {
	args = strings.TrimSpace(args)

	switch {
	case args == "" || args == " list":
		jobs := b.scheduler.List()
		b.reply(msg, FormatJobList(jobs))

	case strings.HasPrefix(args, " add "):
		// Format: /cron add <id> <spec> <label> | <command>
		rest := strings.TrimPrefix(args, " add ")
		parts := strings.SplitN(rest, " | ", 2)
		if len(parts) != 2 {
			b.reply(msg, "Usage: `/cron add <id> <cron-spec> <label> | <command>`\n\nExample:\n`/cron add backup @daily Daily Backup | tar czf backup.tgz /data`")
			return
		}

		header := strings.Fields(parts[0])
		command := strings.TrimSpace(parts[1])

		if len(header) < 2 {
			b.reply(msg, "Need at least: `<id> <spec>`")
			return
		}

		id := header[0]

		// Determine where the spec ends and label begins
		// Specs start with @ or are 5-6 space-separated fields
		var spec, label string
		if strings.HasPrefix(header[1], "@") {
			spec = header[1]
			if len(header) > 2 {
				label = strings.Join(header[2:], " ")
			} else {
				label = id
			}
		} else {
			// Assume 6-field cron spec (with seconds)
			if len(header) >= 7 {
				spec = strings.Join(header[1:7], " ")
				if len(header) > 7 {
					label = strings.Join(header[7:], " ")
				} else {
					label = id
				}
			} else if len(header) >= 6 {
				spec = strings.Join(header[1:6], " ")
				label = id
			} else {
				b.reply(msg, "Invalid cron spec. Use `@every 5m`, `@daily`, or `sec min hour dom mon dow`")
				return
			}
		}

		if err := b.scheduler.Add(id, spec, command, label); err != nil {
			b.reply(msg, "❌ "+err.Error())
			return
		}

		b.reply(msg, fmt.Sprintf("✅ Cron job `%s` created.\nSchedule: `%s`\nCommand: `%s`", id, spec, command))

	case strings.HasPrefix(args, " rm "):
		id := strings.TrimSpace(strings.TrimPrefix(args, " rm "))
		if err := b.scheduler.Remove(id); err != nil {
			b.reply(msg, "❌ "+err.Error())
			return
		}
		b.reply(msg, fmt.Sprintf("🗑 Cron job `%s` removed.", id))

	default:
		b.reply(msg, "Unknown cron command. Use: `/cron list`, `/cron add ...`, `/cron rm <id>`")
	}
}

// Helpers

func (b *Bot) reply(msg *tgbotapi.Message, text string) {
	b.sendMessage(msg.Chat.ID, text)
}

func (b *Bot) sendMessage(chatID int64, text string) {
	// Telegram has a 4096 char limit — split if needed
	chunks := splitMessage(text, 4000)
	for _, chunk := range chunks {
		m := tgbotapi.NewMessage(chatID, chunk)
		m.ParseMode = "Markdown"
		if _, err := b.api.Send(m); err != nil {
			// Retry without markdown if parsing fails
			m.ParseMode = ""
			b.api.Send(m)
		}
	}
}

func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// Try to split at a newline
		idx := strings.LastIndex(text[:maxLen], "\n")
		if idx < maxLen/2 {
			idx = maxLen
		}

		chunks = append(chunks, text[:idx])
		text = text[idx:]
	}
	return chunks
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
