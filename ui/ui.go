/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019 WireGuard LLC. All Rights Reserved.
 */

package ui

import (
	"fmt"
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"golang.zx2c4.com/wireguard/windows/conf"
	"golang.zx2c4.com/wireguard/windows/ringlogger"
	"golang.zx2c4.com/wireguard/windows/service"
	"golang.zx2c4.com/wireguard/windows/ui/syntax"
	"os"
	"runtime"
	"time"
)

const testInterfaceName = "test"

const demoConfig = `
#
# Paste your configuration here
#

[Interface]
PrivateKey = ...
`

const nagMessage = `It looks like you're still using this WireGuard pre-alpha build. Great!

We're glad you like it, and we'd appreciate you sharing both your successes and your tribulations with us via team@wireguard.com or #wireguard on Freenode.

But because this is pre-release software, we're not confident it's something you should yet be using, except for testing and reporting bugs. Check back with us for a newer version.

Would you like to quit WireGuard now? If not, you'll be nagged again in two minutes about the same thing.`

var quit func()

func nag() {
	if walk.MsgBox(nil, "THANKS FOR REPORTING BUGS COME AGAIN ANOTHER DAY", nagMessage, walk.MsgBoxIconError|walk.MsgBoxYesNo|0x00001000) != walk.DlgCmdNo {
		quit()
	}
	time.AfterFunc(time.Minute*2, nag)
}

func RunUI() {
	runtime.LockOSThread()

	icon, _ := walk.NewIconFromResourceId(1)

	mw, _ := walk.NewMainWindowWithName("WireGuard")
	tray, _ := walk.NewNotifyIcon(mw)
	defer tray.Dispose()
	tray.SetIcon(icon)
	tray.SetToolTip("WireGuard: Deactivated")
	tray.SetVisible(true)

	mw.SetSize(walk.Size{900, 1400})
	mw.SetLayout(walk.NewVBoxLayout())
	mw.SetIcon(icon)
	mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = true
		mw.Hide()
	})

	tl, _ := walk.NewTextLabel(mw)
	tl.SetText("Public key: (unknown)")

	se, _ := syntax.NewSyntaxEdit(mw)
	lastPrivate := ""
	se.PrivateKeyChanged().Attach(func(privateKey string) {
		if privateKey == lastPrivate {
			return
		}
		lastPrivate = privateKey
		key, err := conf.NewPrivateKeyFromString(privateKey)
		if err == nil {
			tl.SetText("Public key: " + key.Public().String())
		} else {
			tl.SetText("Public key: (unknown)")
		}
	})

	tunnels, err := service.IPCClientTunnels()
	didFind := false
	if err == nil {
		for _, tunnel := range tunnels {
			if tunnel.Name == testInterfaceName {
				storedConfig, err := tunnel.StoredConfig()
				if err == nil {
					se.SetText(storedConfig.ToWgQuick())
					didFind = true
				}
			}
		}
	}
	if !didFind {
		se.SetText(demoConfig)
	}

	cv, _ := NewConfView(mw)
	cv.SetVisible(false)
	cv.SetEnabled(false)

	var runningTunnel *service.Tunnel
	updateConfView := func() {
		tun := runningTunnel
		if tun == nil || !mw.Visible() || !cv.Visible() {
			return
		}
		conf, err := tun.RuntimeConfig()
		if err != nil {
			return
		}
		cv.SetConfiguration(&conf)
	}
	go func() {
		t := time.NewTicker(time.Second)
		for {
			updateConfView()
			<-t.C
		}
	}()
	showRunningView := func(on bool) {
		cv.SetVisible(on)
		cv.SetEnabled(on)
		se.SetVisible(!on)
		tl.SetVisible(!on)
		if on {
			updateConfView()
		}
	}

	pb, _ := walk.NewPushButton(mw)
	pb.SetText("Start")
	pb.Clicked().Attach(func() {
		restoreState := true
		pbE := pb.Enabled()
		seE := se.Enabled()
		pbT := pb.Text()
		defer func() {
			if restoreState {
				pb.SetEnabled(pbE)
				se.SetEnabled(seE)
				pb.SetText(pbT)
			}
		}()

		mw.SetSuspended(true)
		pb.SetEnabled(false)
		se.SetEnabled(false)
		pb.SetText("Requesting...")
		mw.SetSuspended(false)
		if runningTunnel != nil {
			err := runningTunnel.Stop()
			if err != nil {
				walk.MsgBox(mw, "Unable to stop tunnel", err.Error(), walk.MsgBoxIconError)
				return
			}
			restoreState = false
			return
		}
		c, err := conf.FromWgQuick(se.Text(), testInterfaceName)
		if err != nil {
			walk.MsgBox(mw, "Invalid configuration", err.Error(), walk.MsgBoxIconError)
			return
		}
		cv.SetConfiguration(c)
		tunnel, err := service.IPCClientNewTunnel(c)
		if err != nil {
			walk.MsgBox(mw, "Unable to create tunnel", err.Error(), walk.MsgBoxIconError)
			return
		}
		err = tunnel.Start()
		if err != nil {
			walk.MsgBox(mw, "Unable to start tunnel", err.Error(), walk.MsgBoxIconError)
			return
		}
		restoreState = false
	})

	logger, err := ringlogger.NewRingloggerFromInheritedMappingHandle(os.Args[5], "GUI")
	if err != nil {
		walk.MsgBox(nil, "Unable to initialize logging", fmt.Sprint(err), walk.MsgBoxIconError)
		return
	}
	NewLogView(mw, logger)

	quitAction := walk.NewAction()
	quitAction.SetText("Exit")
	quit = func() {
		tray.Dispose()
		_, err := service.IPCClientQuit(true)
		if err != nil {
			walk.MsgBox(nil, "Error Exiting WireGuard", fmt.Sprintf("Unable to exit service due to: %s. You may want to stop WireGuard from the service manager.", err), walk.MsgBoxIconError)
			os.Exit(1)
		}
	}
	quitAction.Triggered().Attach(quit)
	tray.ContextMenu().Actions().Add(quitAction)
	tray.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			mw.Show()
			win.SetForegroundWindow(mw.Handle())
			win.BringWindowToTop(mw.Handle())
			updateConfView()
		}
	})

	setServiceState := func(tunnel *service.Tunnel, state service.TunnelState, showNotifications bool) {
		if tunnel.Name != testInterfaceName {
			return
		}
		mw.SetSuspended(true)
		//TODO: also set tray icon to reflect state
		switch state {
		case service.TunnelStarting:
			runningTunnel = tunnel
			showRunningView(false)
			se.SetEnabled(false)
			pb.SetText("Starting...")
			pb.SetEnabled(false)
			tray.SetToolTip("WireGuard: Activating...")
		case service.TunnelStarted:
			runningTunnel = tunnel
			showRunningView(true)
			se.SetEnabled(false)
			pb.SetText("Stop")
			pb.SetEnabled(true)
			tray.SetToolTip("WireGuard: Activated")
			if showNotifications {
				//TODO: ShowCustom with right icon
				tray.ShowInfo("WireGuard Activated", fmt.Sprintf("The %s tunnel has been activated.", tunnel.Name))
			}
		case service.TunnelStopping:
			runningTunnel = tunnel
			showRunningView(false)
			se.SetEnabled(false)
			pb.SetText("Stopping...")
			pb.SetEnabled(false)
			tray.SetToolTip("WireGuard: Deactivating...")
		case service.TunnelStopped:
			showRunningView(false)
			se.SetEnabled(true)
			pb.SetText("Start")
			pb.SetEnabled(true)
			tray.SetToolTip("WireGuard: Deactivated")
			if showNotifications && runningTunnel != nil {
				//TODO: ShowCustom with right icon
				tray.ShowInfo("WireGuard Deactivated", fmt.Sprintf("The %s tunnel has been deactivated.", tunnel.Name))
			}
			runningTunnel = nil
		}
		mw.SetSuspended(false)
	}
	service.IPCClientRegisterTunnelChange(func(tunnel *service.Tunnel, state service.TunnelState, err error) {
		setServiceState(tunnel, state, err == nil)
		if err != nil {
			if mw.Visible() {
				errMsg := err.Error()
				if len(errMsg) > 0 && errMsg[len(errMsg)-1] != '.' {
					errMsg += "."
				}
				walk.MsgBox(mw, "Tunnel Error", errMsg+"\n\nPlease consult the log for more information.", walk.MsgBoxIconWarning)
			} else {
				tray.ShowError("WireGuard Tunnel Error", err.Error())
			}
		}
	})
	go func() {
		tunnels, err := service.IPCClientTunnels()
		if err != nil {
			return
		}
		for _, tunnel := range tunnels {
			state, err := tunnel.State()
			if err != nil {
				continue
			}
			if tunnel.Name == testInterfaceName && state != service.TunnelStopped {
				runningTunnel = &tunnel
				setServiceState(&tunnel, state, false)
			}
		}
	}()

	time.AfterFunc(time.Minute*15, nag)

	mw.Run()
}
