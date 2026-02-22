package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

const version = "0.1.0"

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("MiniClaw v%s\n", version)
		os.Exit(0)
	}

	// Banner
	fmt.Println(`
  ╔══════════════════════════════╗
  ║   🐾 MiniClaw v` + version + `         ║
  ║   Poor Man's Remote Agent   ║
  ╚══════════════════════════════╝`)

	// Load config
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("❌ Config error: %s", err)
	}
	log.Printf("✅ Config loaded from %s", *configPath)

	// Initialize Ollama client
	ollama := NewOllamaClient(cfg.Ollama)
	if err := ollama.Ping(); err != nil {
		log.Printf("⚠️  Ollama warning: %s", err)
		log.Printf("   MiniClaw will still work for /exec commands.")
		log.Printf("   Natural language features require Ollama running with model %s", cfg.Ollama.Model)
	} else {
		log.Printf("✅ Ollama connected (%s)", cfg.Ollama.Model)
	}

	// Initialize executor
	executor := NewExecutor(cfg.Executor)
	log.Printf("✅ Workspace: %s", cfg.Executor.Workspace)

	// Initialize bot
	bot, err := NewBot(cfg, ollama, executor)
	if err != nil {
		log.Fatalf("❌ Bot error: %s", err)
	}

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("🛑 Shutting down...")
		// Notify users
		for id := range bot.allowedIDs {
			bot.sendMessage(id, "🛑 MiniClaw shutting down. Goodbye!")
		}
		os.Exit(0)
	}()

	// Start the bot (blocking)
	if err := bot.Start(); err != nil {
		log.Fatalf("❌ Bot error: %s", err)
	}
}
