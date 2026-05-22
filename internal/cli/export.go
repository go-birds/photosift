package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-birds/photosift/internal/analyze"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

var exportOut string

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "path to sqlite db")
	exportCmd.Flags().StringVarP(&exportOut, "out", "o", "photosift-delete.json", "output file for the delete manifest")
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Write the delete manifest (confirmed deletions, or all candidates) as JSON",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer db.Close()

		manifest, err := analyze.DeleteManifest(db)
		if err != nil {
			return err
		}
		b, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(exportOut, b, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %d entries to %s\n", len(manifest), exportOut)
		return nil
	},
}
