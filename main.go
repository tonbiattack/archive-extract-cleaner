package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	userDataPath, err := os.UserCacheDir()
	if err != nil {
		userDataPath = os.TempDir()
	}
	userDataPath = filepath.Join(userDataPath, "archive-extract-cleaner", "webview")
	if err := os.MkdirAll(userDataPath, 0o700); err != nil {
		log.Printf("WebView2のユーザーデータフォルダを作成できません: %v", err)
	}

	err = wails.Run(&options.App{
		Title:                    "ZipSweep",
		Width:                    1360,
		Height:                   860,
		MinWidth:                 900,
		MinHeight:                600,
		BackgroundColour:         &options.RGBA{R: 246, G: 247, B: 249, A: 1},
		AssetServer:              &assetserver.Options{Assets: assets},
		OnStartup:                app.startup,
		OnShutdown:               app.shutdown,
		Bind:                     []interface{}{app},
		EnableDefaultContextMenu: false,
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			WebviewUserDataPath:  userDataPath,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
