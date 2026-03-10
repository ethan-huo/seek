package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type EmbeddingConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions,omitempty"`
}

type Config struct {
	Embedding EmbeddingConfig `yaml:"embedding"`
}

type AppConfig struct {
	Config   Config
	CacheDir string
	DBPath   string
}

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "seek")
}

func cacheDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "seek")
}

func Load() (*AppConfig, error) {
	cfgDir := configDir()
	cacheD := cacheDir()
	os.MkdirAll(cfgDir, 0755)
	os.MkdirAll(cacheD, 0755)

	ac := &AppConfig{
		CacheDir: cacheD,
		DBPath:   filepath.Join(cacheD, "index.db"),
	}

	// Defaults
	ac.Config.Embedding = EmbeddingConfig{
		BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:      "text-embedding-v4",
		Dimensions: 1024,
	}

	cfgPath := filepath.Join(cfgDir, "config.yaml")
	if data, err := os.ReadFile(cfgPath); err == nil {
		if err := yaml.Unmarshal(data, &ac.Config); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	// Resolve env vars in api_key (e.g. ${DASHSCOPE_API_KEY})
	ac.Config.Embedding.APIKey = expandEnv(ac.Config.Embedding.APIKey)

	return ac, nil
}

func (ac *AppConfig) RequireEmbeddingKey() (string, error) {
	key := ac.Config.Embedding.APIKey
	if key == "" {
		return "", fmt.Errorf("embedding API key not configured\nSet DASHSCOPE_API_KEY or add api_key to ~/.config/seek/config.yaml")
	}
	return key, nil
}

func (ac *AppConfig) ConfigPath() string {
	return filepath.Join(configDir(), "config.yaml")
}

func Save(cfg Config) error {
	dir := configDir()
	os.MkdirAll(dir, 0755)

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0600)
}

// expandEnv resolves ${VAR} references in a string.
func expandEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		name := s[2 : len(s)-1]
		return os.Getenv(name)
	}
	return s
}
