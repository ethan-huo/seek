package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/anthropics/seek/internal/config"
)

const (
	plistLabel    = "io.github.ethan-huo.seek"
	plistInterval = 3600 // 1 hour
)

var plistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>/bin/sh</string>
		<string>-c</string>
		<string>{{.Binary}} sync && {{.Binary}} embed</string>
	</array>
	<key>StartInterval</key>
	<integer>{{.Interval}}</integer>
	<key>StandardOutPath</key>
	<string>{{.LogPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{.LogPath}}</string>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`))

type ServiceCmd struct {
	Start  ServiceStartCmd  `cmd:"" help:"Start periodic sync+embed (launchd)"`
	Stop   ServiceStopCmd   `cmd:"" help:"Stop periodic sync+embed"`
	Status ServiceStatusCmd `cmd:"" help:"Show service status"`
}

type ServiceStartCmd struct {
	Interval int `short:"i" default:"3600" help:"Sync interval in seconds"`
}

type ServiceStopCmd struct{}

type ServiceStatusCmd struct{}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")
}

func logPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "seek", "service.log")
}

func seekBinary() string {
	bin, err := exec.LookPath("seek")
	if err != nil {
		// Fallback: assume it's in PATH
		return "seek"
	}
	// Resolve symlinks to get the real path
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return bin
	}
	return real
}

func (c *ServiceStartCmd) Run(cfg *config.AppConfig) error {
	path := plistPath()

	// Ensure log directory exists
	os.MkdirAll(filepath.Dir(logPath()), 0755)

	interval := c.Interval
	if interval < 60 {
		interval = 60
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create plist: %w", err)
	}

	data := struct {
		Label    string
		Binary   string
		Interval int
		LogPath  string
	}{
		Label:    plistLabel,
		Binary:   seekBinary(),
		Interval: interval,
		LogPath:  logPath(),
	}

	if err := plistTemplate.Execute(f, data); err != nil {
		f.Close()
		return fmt.Errorf("write plist: %w", err)
	}
	f.Close()

	// Unload first in case it's already loaded
	exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), path).Run()

	// Load the agent
	out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %s (%w)", string(out), err)
	}

	fmt.Printf("Service started (every %ds)\n", interval)
	fmt.Printf("  Plist: %s\n", path)
	fmt.Printf("  Log:   %s\n", logPath())
	return nil
}

func (c *ServiceStopCmd) Run(cfg *config.AppConfig) error {
	path := plistPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("Service not installed.")
		return nil
	}

	out, err := exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), path).CombinedOutput()
	if err != nil {
		// Ignore "not loaded" errors
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 3 {
			// Already not loaded, just clean up
		} else {
			fmt.Printf("  WARN: launchctl bootout: %s\n", string(out))
		}
	}

	os.Remove(path)
	fmt.Println("Service stopped and removed.")
	return nil
}

func (c *ServiceStatusCmd) Run(cfg *config.AppConfig) error {
	path := plistPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("Service not installed. Run: seek service start")
		return nil
	}

	out, err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), plistLabel)).CombinedOutput()
	if err != nil {
		fmt.Println("Service installed but not running.")
		fmt.Printf("  Plist: %s\n", path)
		return nil
	}

	fmt.Println("Service running.")
	fmt.Printf("  Plist: %s\n", path)
	fmt.Printf("  Log:   %s\n", logPath())

	// Extract useful info from launchctl print output
	lines := string(out)
	_ = lines // launchctl print output is verbose, just confirm it's running
	return nil
}
