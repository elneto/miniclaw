package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type OllamaClient struct {
	baseURL      string
	model        string
	systemPrompt string
	timeout      time.Duration
	httpClient   *http.Client
	// Conversation memory per chat (kept short to fit small context windows)
	history []ChatMessage
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type ChatResponse struct {
	Message      ChatMessage `json:"message"`
	Done         bool        `json:"done"`
	TotalDuration int64     `json:"total_duration,omitempty"`
}

// For streaming partial responses
type StreamChunk struct {
	Message ChatMessage `json:"message"`
	Done    bool        `json:"done"`
}

func NewOllamaClient(cfg OllamaConfig) *OllamaClient {
	return &OllamaClient{
		baseURL:      cfg.URL,
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
		timeout:      time.Duration(cfg.Timeout) * time.Second,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.Timeout) * time.Second,
		},
		history: []ChatMessage{},
	}
}

// Chat sends a message to Ollama and returns the full response (non-streaming).
func (o *OllamaClient) Chat(userMessage string) (string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: o.systemPrompt},
	}

	// Append recent history (keep last 6 exchanges to stay within context)
	maxHistory := 12 // 6 user + 6 assistant
	start := 0
	if len(o.history) > maxHistory {
		start = len(o.history) - maxHistory
	}
	messages = append(messages, o.history[start:]...)
	messages = append(messages, ChatMessage{Role: "user", Content: userMessage})

	req := ChatRequest{
		Model:    o.model,
		Messages: messages,
		Stream:   false,
		Options: map[string]interface{}{
			"temperature":   0.3,
			"num_predict":   2048,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := o.httpClient.Post(o.baseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("calling ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	// Save to history
	o.history = append(o.history, ChatMessage{Role: "user", Content: userMessage})
	o.history = append(o.history, chatResp.Message)

	return chatResp.Message.Content, nil
}

// ChatStream sends a message and streams the response via a callback.
// The callback receives incremental text chunks.
// Returns the full assembled response.
func (o *OllamaClient) ChatStream(userMessage string, onChunk func(string)) (string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: o.systemPrompt},
	}

	maxHistory := 12
	start := 0
	if len(o.history) > maxHistory {
		start = len(o.history) - maxHistory
	}
	messages = append(messages, o.history[start:]...)
	messages = append(messages, ChatMessage{Role: "user", Content: userMessage})

	req := ChatRequest{
		Model:    o.model,
		Messages: messages,
		Stream:   true,
		Options: map[string]interface{}{
			"temperature": 0.3,
			"num_predict": 2048,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := o.httpClient.Post(o.baseURL+"/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("calling ollama: %w", err)
	}
	defer resp.Body.Close()

	var fullResponse strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var chunk StreamChunk
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		fullResponse.WriteString(chunk.Message.Content)
		if onChunk != nil {
			onChunk(chunk.Message.Content)
		}
		if chunk.Done {
			break
		}
	}

	result := fullResponse.String()

	// Save to history
	o.history = append(o.history, ChatMessage{Role: "user", Content: userMessage})
	o.history = append(o.history, ChatMessage{Role: "assistant", Content: result})

	return result, nil
}

// ClearHistory resets conversation memory.
func (o *OllamaClient) ClearHistory() {
	o.history = []ChatMessage{}
}

// ExtractBashCommands finds all ```bash blocks in a response.
var bashBlockRegex = regexp.MustCompile("(?s)```(?:bash|sh)?\n(.*?)```")

func ExtractBashCommands(response string) []string {
	matches := bashBlockRegex.FindAllStringSubmatch(response, -1)
	var commands []string
	for _, m := range matches {
		cmd := strings.TrimSpace(m[1])
		if cmd != "" {
			commands = append(commands, cmd)
		}
	}
	return commands
}

// Ping checks if Ollama is reachable and the model is available.
func (o *OllamaClient) Ping() error {
	resp, err := o.httpClient.Get(o.baseURL + "/api/tags")
	if err != nil {
		return fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decoding models list: %w", err)
	}

	for _, m := range result.Models {
		if m.Name == o.model || strings.HasPrefix(m.Name, o.model) {
			return nil
		}
	}

	available := make([]string, len(result.Models))
	for i, m := range result.Models {
		available[i] = m.Name
	}
	return fmt.Errorf("model %q not found. Available: %s", o.model, strings.Join(available, ", "))
}
