// Command candleshim is Candle.exe in OpenBar's Windows builds. OpenBar was
// Candle up to 0.6.1: that version's updater puts OpenBar.exe beside
// Candle.exe and starts Candle.exe again, and so do the shortcuts its users
// made. This starts OpenBar.exe instead. Build it before mygo build:
//
//	GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H=windowsgui" -o resources/windows-amd64/Candle.exe ./cmd/candleshim
package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	cmd := exec.Command(filepath.Join(filepath.Dir(exe), "OpenBar.exe"), os.Args[1:]...)
	if err := cmd.Start(); err != nil {
		os.Exit(1)
	}
}
