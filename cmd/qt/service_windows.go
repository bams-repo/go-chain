// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func startupShortcutPath(name string) string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, _ := os.UserHomeDir()
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", name+"-wallet.vbs")
}

func installService(exePath, name string) (string, error) {
	shortcut := startupShortcutPath(name)
	if err := os.MkdirAll(filepath.Dir(shortcut), 0755); err != nil {
		return "", fmt.Errorf("create startup dir: %w", err)
	}

	vbs := fmt.Sprintf(`Set WshShell = CreateObject("WScript.Shell")
WshShell.Run """%s""", 0, False
`, exePath)

	if err := os.WriteFile(shortcut, []byte(vbs), 0644); err != nil {
		return "", fmt.Errorf("write startup script: %w", err)
	}
	return "Service installed — wallet will start automatically on login.", nil
}

func uninstallService(name string) (string, error) {
	shortcut := startupShortcutPath(name)
	if err := os.Remove(shortcut); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove startup script: %w", err)
	}
	return "Service removed — wallet will no longer start automatically.", nil
}

func isServiceInstalled(name string) bool {
	_, err := os.Stat(startupShortcutPath(name))
	return err == nil
}

func openFileManager(dir string) error {
	return exec.Command("cmd", "/c", "explorer", dir).Start()
}
