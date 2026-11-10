package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Network Interface Inspector",
		Width:            1120,
		Height:           760,
		MinWidth:         940,
		MinHeight:        620,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 9, G: 11, B: 22, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		Windows: &windows.Options{
			Theme:            windows.Dark,
			ResizeDebounceMS: 50,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
