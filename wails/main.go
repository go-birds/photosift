// PhotoSift desktop shell. This is a thin Wails v2 wrapper that renders the
// existing photosift review UI in a native window instead of a browser tab.
//
// It reuses internal/server unchanged: Wails lets you supply a custom
// http.Handler as the asset server, so the same routes that power `photosift
// serve` (static UI + /api/*) are served straight into the webview. No frontend
// rewrite, no duplicated logic.
package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	"github.com/go-birds/photosift/internal/ingest"
	"github.com/go-birds/photosift/internal/server"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	_ "modernc.org/sqlite"
)

// dbPath mirrors the CLI default (~/.photosift/photosift.db) so the desktop app
// and the `photosift` CLI share one database.
func dbPath() string {
	if p := os.Getenv("PHOTOSIFT_DB"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "photosift.db"
	}
	return filepath.Join(home, ".photosift", "photosift.db")
}

func main() {
	db, err := sql.Open("sqlite", dbPath())
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := ingest.EnsureSchema(db); err != nil {
		log.Fatalf("schema: %v", err)
	}

	handler := server.New(db).Handler()

	err = wails.Run(&options.App{
		Title:     "PhotoSift",
		Width:     1280,
		Height:    900,
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			// Serve the whole app (UI + API) from the existing Go handler.
			Handler: handler,
		},
	})
	if err != nil {
		log.Fatalf("wails: %v", err)
	}
}
