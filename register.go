package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// registerSystemdService installs GoStream as a systemd service
func registerSystemdService(configPath string) {
	// Check if running as root (required for systemd installation)
	if os.Geteuid() != 0 {
		log.Fatalf("Error: -register flag requires sudo\nUsage: sudo ./gostream -c config.json -register")
	}

	// Get the absolute path of the gostream binary
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	// Get current working directory once
	pwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}

	// Get the absolute path of the config file
	absConfigPath := configPath
	if !strings.HasPrefix(configPath, "/") {
		absConfigPath = filepath.Join(pwd, configPath)
	}

	// Check if config file exists
	if _, err := os.Stat(absConfigPath); os.IsNotExist(err) {
		log.Fatalf("Config file not found: %s", absConfigPath)
	}

	// Get the current user running sudo (the one who should own the service)
	currentUser := os.Getenv("SUDO_USER")
	if currentUser == "" {
		currentUser = "root"
	}

	// Create systemd service file
	// Quote paths to handle spaces in directory names
	serviceContent := fmt.Sprintf(`[Unit]
Description=GoStream Server
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
ExecStart="%s" -c "%s"
Restart=always
RestartSec=5
StartLimitBurst=5

LimitNOFILE=65535
LimitNPROC=65535

StandardOutput=journal
StandardError=journal
SyslogIdentifier=gostream

[Install]
WantedBy=multi-user.target
`, currentUser, pwd, exePath, absConfigPath)

	fmt.Println("[register] GoStream Systemd Service Installation")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()
	fmt.Printf("Binary: %s\n", exePath)
	fmt.Printf("Config: %s\n", absConfigPath)
	fmt.Printf("WorkingDirectory: %s\n", pwd)
	fmt.Printf("User: %s\n\n", currentUser)

	// Install systemd service file
	fmt.Println("Installing systemd service...")
	serviceFile := "/etc/systemd/system/gostream.service"

	// Write to temp file first, then copy
	tmpFile := "/tmp/gostream.service"
	if err := os.WriteFile(tmpFile, []byte(serviceContent), 0644); err != nil {
		log.Fatalf("Failed to write temp service file: %v", err)
	}
	defer os.Remove(tmpFile)

	// Copy service file (already running as root)
	cmd := exec.Command("cp", tmpFile, serviceFile)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to copy service file: %v", err)
	}

	// Set permissions
	cmd = exec.Command("chmod", "644", serviceFile)
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to set service file permissions: %v", err)
	}

	// Reload systemd daemon
	fmt.Println("Reloading systemd daemon...")
	cmd = exec.Command("systemctl", "daemon-reload")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to reload systemd: %v", err)
	}

	// Enable service on boot
	fmt.Println("Enabling on boot...")
	cmd = exec.Command("systemctl", "enable", "gostream")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to enable service: %v", err)
	}

	// Start service
	fmt.Println("Starting service...")
	cmd = exec.Command("systemctl", "start", "gostream")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to start service: %v", err)
	}

	// Check status
	fmt.Println()
	fmt.Println("Service status:")
	cmd = exec.Command("systemctl", "status", "gostream", "--no-pager")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("✓ GoStream registered as systemd service!")
	fmt.Println()
	fmt.Println("Service file: " + serviceFile)
	fmt.Println()
	fmt.Println("Useful commands:")
	fmt.Println("  sudo systemctl status gostream")
	fmt.Println("  sudo systemctl restart gostream")
	fmt.Println("  sudo journalctl -u gostream -f")
	fmt.Println()
}
