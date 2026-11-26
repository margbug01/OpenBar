//go:build !signalbot

// Command candle is a candlestick chart of Binance's spot markets.
package main

import (
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // the zones the settings offer, on machines without Go

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans" // MyGo's own controls and menus in Chinese
	_ "github.com/egoist/mygo/locales/zh-Hant"
	"github.com/egoist/mygo/ui"

	"candle/lang"
	"candle/market"
)

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	file := settingsPath()
	// MyGo's updater asks GitHub with http.DefaultClient: the system proxy
	// for it too.
	http.DefaultTransport.(*http.Transport).Proxy = market.SystemProxy
	tr := http.DefaultTransport.(*http.Transport).Clone()
	bin := market.Binance{HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
	var win *mygo.Window
	set := loadSettings(file)
	applyTheme(set.Chart.Theme)
	// Goroutines can post before WhenReady below has made win (seen on
	// Windows); WhenReady queues them until then and runs them after it.
	a := newApplication(set, file, bin, func(fn func()) {
		mygo.App.WhenReady(func() { win.Update(fn) })
	})
	a.upd = myGoUpdates()
	a.icons.dir = iconDir()
	// English for those who read no Chinese; Traditional Chinese readers
	// get OpenBar's Simplified, and MyGo's own controls in Traditional.
	mygo.App.SetLanguages("en", "zh-Hans", "zh-Hant")
	a.lang = language{chosen: mygo.App.ChosenLocale, choose: mygo.App.SetLocale}
	mygo.App.OnLocaleChanged(func(tag string) {
		lang.Set(tag)
		a.setMenu()
	})
	mygo.App.OnSecondInstance(func([]string, string) {
		if win != nil {
			win.Show()
			win.Restore()
			win.Focus()
		}
	})
	mygo.App.WhenReady(func() {
		lang.Set(mygo.App.Locale())
		a.setMenu()
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: "OpenBar", Width: 1320, Height: 820, MinWidth: 960, MinHeight: 560, StateKey: "main",
			TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: titleBarHeight,
			Content: ui.View(a.render),
		})
		a.start()
	})
	if err := mygo.App.Run(); err != nil {
		os.Exit(1)
	}
}
