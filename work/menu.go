package main

import (
	"runtime"

	"github.com/egoist/mygo"

	"candle/lang"
)

// setMenu gives OpenBar a menu bar on macOS, in the language it shows, with
// the settings in the app menu; it is set again when the language changes.
// Windows has none: the title bar is the app's own.
//
// It has no Edit menu. Its Undo, Copy and Paste would take Cmd+Z, Cmd+C and
// Cmd+V before the window sees them and hand them only to a text input with
// the focus, so the drawings' undo, copy and paste would never get them.
// Without it the keys reach the window as they do on Windows: a text input
// with the focus takes them first, the drawings the rest.
func (a *application) setMenu() {
	if runtime.GOOS != "darwin" {
		return
	}
	settings := func(*mygo.MenuItem, *mygo.Window) {
		a.post(func() {
			if !a.dialog && !a.indList && !a.indDialog {
				a.openSettings()
			}
		})
	}
	checkUpdate := func(*mygo.MenuItem, *mygo.Window) {
		a.post(func() {
			a.openAbout()
			if a.dialog {
				a.checkUpdate(true)
			}
		})
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "OpenBar", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout, Label: lang.T("关于 OpenBar")},
			{Label: lang.T("检查更新…"), Click: checkUpdate},
			mygo.Separator(),
			{Label: lang.T("设置…"), Accelerator: "CmdOrCtrl+,", Click: settings},
			mygo.Separator(),
			{Role: mygo.RoleServices, Label: lang.T("服务")},
			mygo.Separator(),
			{Role: mygo.RoleHide, Label: lang.T("隐藏 OpenBar")},
			{Role: mygo.RoleHideOthers, Label: lang.T("隐藏其他")},
			{Role: mygo.RoleUnhide, Label: lang.T("全部显示")},
			mygo.Separator(),
			{Role: mygo.RoleQuit, Label: lang.T("退出 OpenBar")},
		}},
		{Label: lang.T("显示"), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleToggleFullScreen, Label: lang.T("进入全屏幕")},
		}},
		{Label: lang.T("窗口"), Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleMinimize, Label: lang.T("最小化")},
			{Role: mygo.RoleZoom, Label: lang.T("缩放")},
			mygo.Separator(),
			{Role: mygo.RoleClose, Label: lang.T("关闭")},
			{Role: mygo.RoleFront, Label: lang.T("前置全部窗口")},
		}},
	}))
}
