// Copyright (c) 2024-2026 The Fairchain Contributors
// Fairchain is an experiment in modularity, designed to improve on the work
// of Satoshi Nakamoto and to inspire more creative genius in the space.
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/bams-repo/fairchain/internal/coinparams"
	"github.com/bams-repo/fairchain/internal/version"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIconPNG []byte

//go:embed assets/trayicon.png
var trayIconPNG []byte

func main() {
	app := NewApp()

	if err := wails.Run(&options.App{
		Title:             coinparams.Name + " Wallet",
		Width:             1200,
		Height:            800,
		MinWidth:          900,
		MinHeight:         600,
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
		Linux: &linux.Options{
			Icon: appIconPNG,
		},
		Mac: &mac.Options{
			About: &mac.AboutInfo{
				Title:   coinparams.Name + " Wallet",
				Message: "Version " + version.String(),
				Icon:    appIconPNG,
			},
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%s Wallet v%s: %v\n", coinparams.Name, version.String(), err)
		os.Exit(1)
	}
}
