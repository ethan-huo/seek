package cmd

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/anthropics/seek/internal/config"
	"golang.org/x/term"
)

type AuthCmd struct {
	Login  AuthLoginCmd  `cmd:"" help:"Configure API key"`
	Status AuthStatusCmd `cmd:"" help:"Show auth status"`
}

type AuthLoginCmd struct{}

type AuthStatusCmd struct{}

type provider struct {
	Name    string
	BaseURL string
	Model   string
}

var providers = []provider{
	{
		Name:    "dashscope (阿里百炼)",
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "text-embedding-v4",
	},
	{
		Name:    "openai",
		BaseURL: "https://api.openai.com/v1",
		Model:   "text-embedding-3-small",
	},
	{
		Name:    "custom (OpenAI-compatible)",
		BaseURL: "",
		Model:   "",
	},
}

func (c *AuthLoginCmd) Run(cfg *config.AppConfig) error {
	fmt.Println("\nSelect embedding provider:")
	for i, p := range providers {
		fmt.Printf("  %d) %s\n", i+1, p.Name)
	}
	fmt.Print("\nChoice [1]: ")

	choiceStr := readLine()
	choice := 0
	if choiceStr != "" {
		fmt.Sscanf(choiceStr, "%d", &choice)
		choice--
	}
	if choice < 0 || choice >= len(providers) {
		choice = 0
	}

	p := providers[choice]
	baseURL := p.BaseURL
	model := p.Model

	if p.BaseURL == "" {
		fmt.Print("Base URL: ")
		baseURL = readLine()

		fmt.Print("Model name: ")
		model = readLine()
	}

	fmt.Print("\nAPI Key (input hidden): ")
	keyBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read key: %w", err)
	}
	apiKey := strings.TrimSpace(string(keyBytes))
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	newCfg := config.Config{
		Embedding: config.EmbeddingConfig{
			BaseURL: baseURL,
			APIKey:  apiKey,
			Model:   model,
		},
	}

	if err := config.Save(newCfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nSaved to %s\n", cfg.ConfigPath())
	fmt.Printf("  Provider: %s\n", providers[choice].Name)
	fmt.Printf("  Model:    %s\n", model)
	fmt.Printf("  Key:      %s...%s\n", apiKey[:4], apiKey[len(apiKey)-4:])

	return nil
}

func (c *AuthStatusCmd) Run(cfg *config.AppConfig) error {
	key := cfg.Config.Embedding.APIKey
	if key == "" {
		fmt.Println("Not configured. Run: seek auth login")
		return nil
	}

	masked := key[:4] + "..." + key[len(key)-4:]
	fmt.Printf("Provider:  %s\n", cfg.Config.Embedding.BaseURL)
	fmt.Printf("Model:     %s\n", cfg.Config.Embedding.Model)
	fmt.Printf("API Key:   %s\n", masked)
	return nil
}

// readLine reads a line in raw mode, handling ESC, backspace, and Ctrl-C cleanly.
func readLine() string {
	fd := int(syscall.Stdin)
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Fallback: just read without raw mode
		var buf [256]byte
		n, _ := os.Stdin.Read(buf[:])
		return strings.TrimSpace(string(buf[:n]))
	}
	defer term.Restore(fd, oldState)

	var line []byte
	buf := make([]byte, 16) // big enough for any escape sequence
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}
		// If we read multiple bytes at once, it's likely an escape sequence — skip it
		if n > 1 {
			continue
		}
		ch := buf[0]
		switch {
		case ch == '\r' || ch == '\n':
			os.Stdout.Write([]byte("\r\n"))
			return string(line)
		case ch == 3: // Ctrl-C
			os.Stdout.Write([]byte("^C\r\n"))
			os.Exit(130)
		case ch == 27: // Bare ESC — ignore
			continue
		case ch == 127 || ch == 8: // Backspace / DEL
			if len(line) > 0 {
				line = line[:len(line)-1]
				os.Stdout.Write([]byte("\b \b"))
			}
		case ch >= 32 && ch < 127: // Printable ASCII
			line = append(line, ch)
			os.Stdout.Write([]byte{ch})
		}
	}
	return string(line)
}
