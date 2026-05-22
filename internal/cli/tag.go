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

var tagFile string

func init() {
	rootCmd.AddCommand(tagCmd)
	tagCmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "path to sqlite db")
	tagCmd.Flags().StringVarP(&tagFile, "in", "i", "labels.json", "JSON file of [{path,label,score}] from the ML classifier")
}

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Import semantic content labels (from ml/classify.py) for 'useless content' detection",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		b, err := os.ReadFile(tagFile)
		if err != nil {
			return err
		}
		var labels []analyze.Label
		if err := json.Unmarshal(b, &labels); err != nil {
			return fmt.Errorf("parse %s: %w", tagFile, err)
		}

		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer db.Close()

		n, err := analyze.ApplyLabels(db, labels)
		if err != nil {
			return err
		}
		fmt.Printf("applied %d labels (of %d) — re-run 'photosift analyze' to use them\n", n, len(labels))
		return nil
	},
}
