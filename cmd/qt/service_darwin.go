// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func plistPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com."+name+".wallet.plist")
}

func installService(exePath, name string) (string, error) {
	pp := plistPath(name)
	if err := os.MkdirAll(filepath.Dir(pp), 0755); err != nil {
		return "", fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.%s.wallet</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <false/>
</dict>
</plist>
`, name, exePath)

	if err := os.WriteFile(pp, []byte(plist), 0644); err != nil {
		return "", fmt.Errorf("write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", pp).Run(); err != nil {
		return "", fmt.Errorf("launchctl load: %w", err)
	}

	return "Service installed — wallet will start automatically on login.", nil
}

func uninstallService(name string) (string, error) {
	pp := plistPath(name)
	_ = exec.Command("launchctl", "unload", pp).Run()

	if err := os.Remove(pp); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove plist: %w", err)
	}
	return "Service removed — wallet will no longer start automatically.", nil
}

func isServiceInstalled(name string) bool {
	_, err := os.Stat(plistPath(name))
	return err == nil
}

func openFileManager(dir string) error {
	return exec.Command("open", dir).Start()
}
