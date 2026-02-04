// scan.go

package cli

import(
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-birds/photosift/internal/ingest"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

var dbPath string

func init() {
	rootCmd.AddCommand(scanCmd)
	scanCmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "path to sqlite db")
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./photosift.db"
	}
	return filepath.Join(home, ".photosift", "photosift.db")
}

var scanCmd = &cobra.Command{
	Use: "scan [path...]",
	Short: "Scan one or more directories and index images",
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command,args []string) error {
		start := time.Now()

		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return fmt.Errorf("open db: %w", err)
		}

		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open db: %q", err)
		}
		defer db.Close()

		if err := ingest.EnsureSchema(db); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}

		count, err := ingest.ScanPaths(db, args)
		if err != nil {
			return err
		}

		fmt.Printf("scanned %d images in %s\n", count, time.Since(start))
		fmt.Printf("db: %s\n", dbPath)
		return nil
	},
}
