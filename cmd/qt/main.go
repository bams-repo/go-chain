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
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// defaultNetwork is set at build time via -ldflags:
//
//	-X main.defaultNetwork=testnet
//
// Falls back to "testnet" when unset (e.g. during `wails dev`).
var defaultNetwork string

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIconPNG []byte

//go:embed assets/trayicon.png
var trayIconPNG []byte

func buildAppMenu(app *App) *menu.Menu {
	appMenu := menu.NewMenu()

	fileMenu := appMenu.AddSubmenu("File")
	fileMenu.AddText("Quit", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
		wailsRuntime.Quit(app.ctx)
	})

	miningMenu := appMenu.AddSubmenu("Mining")
	miningMenu.AddText("Start Mining", keys.CmdOrCtrl("m"), func(_ *menu.CallbackData) {
		wailsRuntime.EventsEmit(app.ctx, "menu:toggle-mining")
	})

	walletMenu := appMenu.AddSubmenu("Wallet")
	walletMenu.AddText("Encrypt Wallet...", nil, func(_ *menu.CallbackData) {
		wailsRuntime.EventsEmit(app.ctx, "menu:encrypt-wallet")
	})
	walletMenu.AddText("Change Passphrase...", nil, func(_ *menu.CallbackData) {
		wailsRuntime.EventsEmit(app.ctx, "menu:change-passphrase")
	})
	walletMenu.AddSeparator()
	walletMenu.AddText("Sign Message...", nil, func(_ *menu.CallbackData) {
		wailsRuntime.EventsEmit(app.ctx, "menu:sign-message")
	})
	walletMenu.AddText("Verify Message...", nil, func(_ *menu.CallbackData) {
		wailsRuntime.EventsEmit(app.ctx, "menu:verify-message")
	})

	helpMenu := appMenu.AddSubmenu("Help")
	helpMenu.AddText("About "+coinparams.Name+" Wallet", nil, func(_ *menu.CallbackData) {
		_, _ = wailsRuntime.MessageDialog(app.ctx, wailsRuntime.MessageDialogOptions{
			Type:    wailsRuntime.InfoDialog,
			Title:   "About " + coinparams.Name + " Wallet",
			Message: coinparams.Name + " Wallet v" + version.String() + "\n\n" + coinparams.CopyrightHolder + "\nDistributed under the MIT software license.",
		})
	})
	helpMenu.AddText("Debug Window", keys.Key("f12"), func(_ *menu.CallbackData) {
		wailsRuntime.EventsEmit(app.ctx, "menu:debug-window")
	})

	return appMenu
}

func networkForBuild() string {
	if defaultNetwork == "" {
		return "testnet"
	}
	return defaultNetwork
}

func windowTitle() string {
	net := networkForBuild()
	if net == "mainnet" {
		return coinparams.Name + " Wallet"
	}
	return coinparams.Name + " Wallet [" + net + "]"
}

func main() {
	app := NewApp()

	if err := wails.Run(&options.App{
		Title:             windowTitle(),
		Width:             1200,
		Height:            800,
		MinWidth:          900,
		MinHeight:         600,
		HideWindowOnClose: true,
		Menu:              buildAppMenu(app),
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
				Title:   windowTitle(),
				Message: "Version " + version.String(),
				Icon:    appIconPNG,
			},
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%s Wallet v%s: %v\n", coinparams.Name, version.String(), err)
		os.Exit(1)
	}
}
