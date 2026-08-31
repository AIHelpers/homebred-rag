// Command desktop is the Wails GUI shell for HomeBred-RAG: the same
// ingest/retrieve/answer use cases the CLI (cmd/cli) drives, behind a
// desktop window instead of a terminal. See app.go for the bound backend
// methods and frontend/dist for the UI.
//
// Build:
//
//	go build -tags desktop,production -o bin/homebredrag-desktop ./cmd/desktop
//
// On Linux this needs GTK3 + WebKit2GTK dev headers at build time (and the
// runtime libraries wherever the binary runs):
//
//	apt install libgtk-3-dev libwebkit2gtk-4.1-dev
//	go build -tags desktop,production,webkit2_41 -o bin/homebredrag-desktop ./cmd/desktop
//
// (Use webkit2_41 on newer distros — Ubuntu 24.04+, Debian 13+ — that ship
// webkit2gtk 4.1 instead of the older 4.0. Omit the tag on older distros
// that still package webkit2gtk-4.0.)
//
// Add -tags ocr to also enable scanned-document OCR in the desktop build,
// e.g. -tags desktop,production,webkit2_41,ocr.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "HomeBred-RAG",
		Width:  1100,
		Height: 760,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("error:", err.Error())
	}
}
