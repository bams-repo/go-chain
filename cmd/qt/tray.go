// Copyright (c) 2024-2026 The Fairchain Contributors
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"github.com/energye/systray"
)

type trayCallbacks struct {
	OnShow func()
	OnQuit func()
}

func initTray(iconPNG []byte, tooltip string, cb trayCallbacks) (start, end func()) {
	onReady := func() {
		systray.SetIcon(iconPNG)
		systray.SetTitle(tooltip)
		systray.SetTooltip(tooltip)

		systray.SetOnClick(func(menu systray.IMenu) {
			if cb.OnShow != nil {
				cb.OnShow()
			}
		})

		systray.SetOnDClick(func(menu systray.IMenu) {
			if cb.OnShow != nil {
				cb.OnShow()
			}
		})

		mShow := systray.AddMenuItem("Show Wallet", "Restore the wallet window")
		mShow.Click(func() {
			if cb.OnShow != nil {
				cb.OnShow()
			}
		})

		systray.AddSeparator()

		mQuit := systray.AddMenuItem("Quit", "Shut down the wallet completely")
		mQuit.Click(func() {
			if cb.OnQuit != nil {
				cb.OnQuit()
			}
		})
	}

	onExit := func() {}

	return systray.RunWithExternalLoop(onReady, onExit)
}
