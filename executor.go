package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Executor struct {
	workspace      string
	timeout        time.Duration
	maxOutputBytes int
}

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	Truncated bool
}

func NewExecutor(cfg ExecutorConfig) *Executor {
	return &Executor{
		workspace:      cfg.Workspace,
		timeout:        time.Duration(cfg.Timeout) * time.Second,
		maxOutputBytes: cfg.MaxOutputBytes,
	}
}

// Run executes a bash command string in the workspace directory.
func (e *Executor) Run(command string) (*ExecResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = e.workspace
	cmd.Env = append(os.Environ(),
		"MINICLAW=1",
		"WORKSPACE="+e.workspace,
	)

	start := time.Now()

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.ExitCode = -1
		result.Stderr += "\n⏱ TIMEOUT: command exceeded " + e.timeout.String()
		return result, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("executing command: %w", err)
		}
	}

	// Truncate large outputs
	if len(result.Stdout) > e.maxOutputBytes {
		result.Stdout = result.Stdout[:e.maxOutputBytes] + "\n... [truncated]"
		result.Truncated = true
	}
	if len(result.Stderr) > e.maxOutputBytes {
		result.Stderr = result.Stderr[:e.maxOutputBytes] + "\n... [truncated]"
		result.Truncated = true
	}

	return result, nil
}

// RunScript executes a script file from the workspace.
func (e *Executor) RunScript(filename string, args ...string) (*ExecResult, error) {
	path := filepath.Join(e.workspace, filename)

	// Check file exists
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("script not found: %s", filename)
	}

	// Make executable if not already
	if info.Mode()&0111 == 0 {
		os.Chmod(path, info.Mode()|0755)
	}

	// Determine interpreter from shebang or extension
	interpreter := detectInterpreter(path, filename)
	cmdStr := interpreter + " " + path
	if len(args) > 0 {
		cmdStr += " " + strings.Join(args, " ")
	}

	return e.Run(cmdStr)
}

// SaveFile saves content to the workspace.
func (e *Executor) SaveFile(filename string, content []byte) (string, error) {
	// Sanitize filename — no path traversal
	filename = filepath.Base(filename)
	path := filepath.Join(e.workspace, filename)

	if err := os.WriteFile(path, content, 0644); err != nil {
		return "", fmt.Errorf("saving file: %w", err)
	}

	return path, nil
}

// ListFiles lists files in the workspace.
func (e *Executor) ListFiles() ([]FileInfo, error) {
	entries, err := os.ReadDir(e.workspace)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:    entry.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   entry.IsDir(),
		})
	}
	return files, nil
}

// ReadFile reads a file from the workspace.
func (e *Executor) ReadFile(filename string) (string, error) {
	filename = filepath.Base(filename)
	path := filepath.Join(e.workspace, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	content := string(data)
	if len(content) > 4000 {
		content = content[:4000] + "\n... [truncated]"
	}
	return content, nil
}

// DeleteFile removes a file from the workspace.
func (e *Executor) DeleteFile(filename string) error {
	filename = filepath.Base(filename)
	path := filepath.Join(e.workspace, filename)
	return os.Remove(path)
}

type FileInfo struct {
	Name    string
	Size    int64
	ModTime time.Time
	IsDir   bool
}

func detectInterpreter(path, filename string) string {
	// Try reading shebang
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 2 && string(data[:2]) == "#!" {
		line := strings.SplitN(string(data), "\n", 2)[0]
		return strings.TrimPrefix(line, "#!")
	}

	// Fall back to extension
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".py":
		return "python3"
	case ".sh", ".bash":
		return "bash"
	case ".js":
		return "node"
	case ".rb":
		return "ruby"
	case ".pl":
		return "perl"
	default:
		return "bash"
	}
}

// FormatResult formats an execution result for Telegram display.
func FormatResult(r *ExecResult) string {
	var sb strings.Builder

	if r.ExitCode == 0 {
		sb.WriteString("✅ Success")
	} else {
		sb.WriteString(fmt.Sprintf("❌ Exit code: %d", r.ExitCode))
	}
	sb.WriteString(fmt.Sprintf(" (%.1fs)\n", r.Duration.Seconds()))

	if r.Stdout != "" {
		sb.WriteString("\n📤 stdout:\n```\n")
		sb.WriteString(r.Stdout)
		sb.WriteString("\n```")
	}

	if r.Stderr != "" {
		sb.WriteString("\n📛 stderr:\n```\n")
		sb.WriteString(r.Stderr)
		sb.WriteString("\n```")
	}

	if r.Stdout == "" && r.Stderr == "" {
		sb.WriteString("\n(no output)")
	}

	if r.Truncated {
		sb.WriteString("\n⚠️ Output was truncated")
	}

	return sb.String()
}
