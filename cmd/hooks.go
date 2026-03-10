package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/anthropics/seek/internal/config"
	"golang.org/x/term"
)

type HooksCmd struct {
	Install   HooksInstallCmd   `cmd:"" help:"Install hooks into AI tools"`
	Uninstall HooksUninstallCmd `cmd:"" help:"Remove seek hooks"`
}

type HooksInstallCmd struct {
	Claude bool `help:"Install Claude Code Stop hook"`
}

type HooksUninstallCmd struct {
	Claude bool `help:"Remove Claude Code Stop hook"`
}

const seekHookCommand = "seek sync"

// claudeSettingsPath returns the path to Claude Code settings.json.
func claudeSettingsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func (c *HooksInstallCmd) Run(cfg *config.AppConfig) error {
	// If no flags, show interactive TUI
	if !c.Claude {
		return c.interactive()
	}

	if c.Claude {
		return installClaudeHook()
	}
	return nil
}

func (c *HooksInstallCmd) interactive() error {
	type hookOption struct {
		Name      string
		Installed bool
		Install   func() error
	}

	options := []hookOption{
		{
			Name:      "Claude Code (Stop hook → seek sync)",
			Installed: isClaudeHookInstalled(),
			Install:   installClaudeHook,
		},
	}

	fmt.Println("\nAvailable hooks:")
	for i, opt := range options {
		status := "  "
		if opt.Installed {
			status = "✓ "
		}
		fmt.Printf("  %s%d) %s\n", status, i+1, opt.Name)
	}
	fmt.Println()

	// Read selections in raw mode
	fmt.Print("Toggle (1-9, Enter to confirm): ")
	selected := make([]bool, len(options))
	for i, opt := range options {
		selected[i] = opt.Installed
	}

	fd := int(syscall.Stdin)
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Fallback: just install all
		for _, opt := range options {
			if !opt.Installed {
				opt.Install()
			}
		}
		return nil
	}

	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}
		if n > 1 {
			continue // escape sequence
		}
		ch := buf[0]

		switch {
		case ch == '\r' || ch == '\n':
			term.Restore(fd, oldState)
			os.Stdout.Write([]byte("\r\n"))
			goto done
		case ch == 3: // Ctrl-C
			term.Restore(fd, oldState)
			os.Stdout.Write([]byte("\r\n"))
			return nil
		case ch == 27: // ESC
			continue
		case ch >= '1' && ch <= '9':
			idx := int(ch-'1')
			if idx < len(options) {
				selected[idx] = !selected[idx]
				// Redraw
				term.Restore(fd, oldState)
				// Move cursor up and redraw
				fmt.Printf("\r\033[%dA", len(options)+2)
				fmt.Println("\nAvailable hooks:")
				for i, opt := range options {
					status := "  "
					if selected[i] {
						status = "✓ "
					}
					fmt.Printf("  %s%d) %s\n", status, i+1, opt.Name)
				}
				fmt.Println()
				fmt.Print("Toggle (1-9, Enter to confirm): ")
				oldState, _ = term.MakeRaw(fd)
			}
		}
	}

done:
	var installed, removed int
	for i, opt := range options {
		if selected[i] && !opt.Installed {
			if err := opt.Install(); err != nil {
				fmt.Printf("  ERROR installing %s: %v\n", opt.Name, err)
			} else {
				installed++
			}
		} else if !selected[i] && opt.Installed {
			// Uninstall
			if strings.Contains(opt.Name, "Claude") {
				uninstallClaudeHook()
			}
			removed++
		}
	}

	if installed > 0 {
		fmt.Printf("Installed %d hook(s).\n", installed)
	}
	if removed > 0 {
		fmt.Printf("Removed %d hook(s).\n", removed)
	}
	if installed == 0 && removed == 0 {
		fmt.Println("No changes.")
	}

	return nil
}

func (c *HooksUninstallCmd) Run(cfg *config.AppConfig) error {
	if !c.Claude {
		// Default: uninstall all
		c.Claude = true
	}

	if c.Claude {
		if err := uninstallClaudeHook(); err != nil {
			return err
		}
	}
	return nil
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

func isClaudeHookInstalled() bool {
	settings, err := readClaudeSettings()
	if err != nil {
		return false
	}
	return findSeekHookIndex(settings) >= 0
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

	// Remove the seek entry
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
