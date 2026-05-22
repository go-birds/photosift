package cli

import (
	"database/sql"
	"fmt"

	"github.com/go-birds/photosift/internal/server"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

var serveAddr string

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "path to sqlite db")
	serveCmd.Flags().StringVar(&serveAddr, "addr", "127.0.0.1:8765", "address to bind the review UI")
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Open the review UI to inspect and mark suggested deletions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer db.Close()
		return server.Serve(db, serveAddr)
	},
}
