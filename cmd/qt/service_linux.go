// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func serviceUnitPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", name+"-wallet.service")
}

func installService(exePath, name string) (string, error) {
	unitPath := serviceUnitPath(name)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0755); err != nil {
		return "", fmt.Errorf("create systemd dir: %w", err)
	}

	unit := fmt.Sprintf(`[Unit]
Description=%s Wallet
After=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, name, exePath)

	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return "", fmt.Errorf("write unit file: %w", err)
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return "", fmt.Errorf("daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", name+"-wallet.service").Run(); err != nil {
		return "", fmt.Errorf("enable service: %w", err)
	}

	return "Service installed — wallet will start automatically on login.", nil
}

func uninstallService(name string) (string, error) {
	svc := name + "-wallet.service"
	_ = exec.Command("systemctl", "--user", "disable", svc).Run()
	_ = exec.Command("systemctl", "--user", "stop", svc).Run()

	unitPath := serviceUnitPath(name)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove unit file: %w", err)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	return "Service removed — wallet will no longer start automatically.", nil
}

func isServiceInstalled(name string) bool {
	_, err := os.Stat(serviceUnitPath(name))
	return err == nil
}

func openFileManager(dir string) error {
	return exec.Command("xdg-open", dir).Start()
}
