package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram  TelegramConfig  `yaml:"telegram"`
	Ollama    OllamaConfig    `yaml:"ollama"`
	Executor  ExecutorConfig  `yaml:"executor"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

type TelegramConfig struct {
	Token      string  `yaml:"token"`
	AllowedIDs []int64 `yaml:"allowed_ids"`
}

type OllamaConfig struct {
	URL          string `yaml:"url"`
	Model        string `yaml:"model"`
	SystemPrompt string `yaml:"system_prompt"`
	AutoExecute  bool   `yaml:"auto_execute"`
	Timeout      int    `yaml:"timeout_seconds"`
}

type ExecutorConfig struct {
	Workspace      string `yaml:"workspace"`
	Timeout        int    `yaml:"timeout_seconds"`
	MaxOutputBytes int    `yaml:"max_output_bytes"`
}

type SchedulerConfig struct {
	PersistFile string `yaml:"persist_file"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{
		Ollama: OllamaConfig{
			URL:     "http://localhost:11434",
			Model:   "llama3.2:3b",
			Timeout: 120,
			SystemPrompt: `You are MiniClaw, a system administration assistant running on the user's machine.
When the user asks you to perform a task, respond with the necessary bash commands wrapped in triple-backtick bash blocks like:
` + "```bash" + `
command here
` + "```" + `
Always explain briefly what each command does.
If the task is dangerous (rm -rf, format, etc.), warn the user clearly.
If you need multiple commands, put them in a single block separated by newlines.
Keep explanations concise — the user sees this on a phone screen.`,
		},
		Executor: ExecutorConfig{
			Workspace:      "~/.miniclaw/workspace",
			Timeout:        60,
			MaxOutputBytes: 4000,
		},
		Scheduler: SchedulerConfig{
			PersistFile: "~/.miniclaw/crontab.json",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Expand ~ in paths
	home, _ := os.UserHomeDir()
	cfg.Executor.Workspace = expandHome(cfg.Executor.Workspace, home)
	cfg.Scheduler.PersistFile = expandHome(cfg.Scheduler.PersistFile, home)

	// Create workspace directory
	if err := os.MkdirAll(cfg.Executor.Workspace, 0755); err != nil {
		return nil, fmt.Errorf("creating workspace: %w", err)
	}

	// Validate
	if cfg.Telegram.Token == "" {
		return nil, fmt.Errorf("telegram.token is required")
	}
	if len(cfg.Telegram.AllowedIDs) == 0 {
		return nil, fmt.Errorf("telegram.allowed_ids must have at least one user ID")
	}

	return cfg, nil
}

func expandHome(path, home string) string {
	if len(path) > 0 && path[0] == '~' {
		return home + path[1:]
	}
	return path
}
