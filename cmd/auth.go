package cmd

import (
	"bufio"
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
	EnvVar  string
}

var providers = []provider{
	{
		Name:    "dashscope (阿里百炼)",
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "text-embedding-v4",
		EnvVar:  "DASHSCOPE_API_KEY",
	},
	{
		Name:    "openai",
		BaseURL: "https://api.openai.com/v1",
		Model:   "text-embedding-3-small",
		EnvVar:  "OPENAI_API_KEY",
	},
	{
		Name:    "custom (OpenAI-compatible)",
		BaseURL: "",
		Model:   "",
		EnvVar:  "",
	},
}

func (c *AuthLoginCmd) Run(cfg *config.AppConfig) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\nSelect embedding provider:")
	for i, p := range providers {
		fmt.Printf("  %d) %s\n", i+1, p.Name)
	}
	fmt.Print("\nChoice [1]: ")

	choiceStr, _ := reader.ReadString('\n')
	choiceStr = strings.TrimSpace(choiceStr)
	choice := 0
	if choiceStr == "" {
		choice = 0
	} else {
		fmt.Sscanf(choiceStr, "%d", &choice)
		choice--
	}
	if choice < 0 || choice >= len(providers) {
		choice = 0
	}

	p := providers[choice]
	baseURL := p.BaseURL
	model := p.Model

	// Custom provider needs URL + model
	if p.BaseURL == "" {
		fmt.Print("Base URL: ")
		baseURL, _ = reader.ReadString('\n')
		baseURL = strings.TrimSpace(baseURL)

		fmt.Print("Model name: ")
		model, _ = reader.ReadString('\n')
		model = strings.TrimSpace(model)
	}

	fmt.Printf("\nAPI Key (input hidden): ")
	keyBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("read key: %w", err)
	}
	apiKey := strings.TrimSpace(string(keyBytes))
	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	// Save to config
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
