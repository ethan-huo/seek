package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anthropics/seek/internal/config"
)

type HooksCmd struct {
	Install   HooksInstallCmd   `cmd:"" help:"Install seek hooks into AI tools"`
	Uninstall HooksUninstallCmd `cmd:"" help:"Remove seek hooks from AI tools"`
}

type HooksInstallCmd struct{}

type HooksUninstallCmd struct{}

// claudeSettingsPath returns the path to Claude Code settings.json.
func claudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func (c *HooksInstallCmd) Run(cfg *config.AppConfig) error {
	return installClaudeHook()
}

func (c *HooksUninstallCmd) Run(cfg *config.AppConfig) error {
	return uninstallClaudeHook()
}

// --- Claude Code hooks ---

func readClaudeSettings() (map[string]interface{}, error) {
	path := claudeSettingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, err
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse settings.json: %w", err)
	}
	return settings, nil
}

func writeClaudeSettings(settings map[string]interface{}) error {
	path := claudeSettingsPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func seekBinaryPath() string {
	bin, err := exec.LookPath("seek")
	if err != nil {
		return "seek"
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return bin
	}
	return real
}

func findSeekHookIndex(settings map[string]interface{}) int {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return -1
	}
	stopHooks, ok := hooks["Stop"].([]interface{})
	if !ok {
		return -1
	}
	for i, entry := range stopHooks {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		hookList, ok := entryMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hookList {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, _ := hMap["command"].(string)
			if strings.Contains(cmd, "seek sync") {
				return i
			}
		}
	}
	return -1
}

func installClaudeHook() error {
	settings, err := readClaudeSettings()
	if err != nil {
		return err
	}

	if findSeekHookIndex(settings) >= 0 {
		fmt.Println("Claude Code hook already installed.")
		return nil
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	stopHooks, ok := hooks["Stop"].([]interface{})
	if !ok {
		stopHooks = []interface{}{}
	}

	newHook := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": seekBinaryPath() + " sync",
			},
		},
	}

	stopHooks = append(stopHooks, newHook)
	hooks["Stop"] = stopHooks

	if err := writeClaudeSettings(settings); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	fmt.Println("Installed Claude Code Stop hook → seek sync")
	return nil
}

func uninstallClaudeHook() error {
	settings, err := readClaudeSettings()
	if err != nil {
		return err
	}

	idx := findSeekHookIndex(settings)
	if idx < 0 {
		fmt.Println("Claude Code hook not installed.")
		return nil
	}

	hooks := settings["hooks"].(map[string]interface{})
	stopHooks := hooks["Stop"].([]interface{})

	stopHooks = append(stopHooks[:idx], stopHooks[idx+1:]...)
	if len(stopHooks) == 0 {
		delete(hooks, "Stop")
	} else {
		hooks["Stop"] = stopHooks
	}

	if err := writeClaudeSettings(settings); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	fmt.Println("Removed Claude Code Stop hook.")
	return nil
}
