// dupes.go

package cli

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

var maxDist int

func init() {
	dupesCmd.Flags().IntVarP(&maxDist, "max-dist", "d", 6, "maximum Hamming distance to consider images as duplicates")
	rootCmd.AddCommand(dupesCmd)
}

var dupesCmd = &cobra.Command{
	Use:   "dupes",
	Short: "List exact duplicate images (by sha256)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbp := dbPath
		if dbp == "" {
			dbp = defaultDBPath()
		}

		db, err := sql.Open("sqlite", dbp)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer db.Close()

		rows, err := db.Query(`SELECT sha256, COUNT(*) FROM images GROUP BY sha256 HAVING COUNT(*) > 1`)
		if err != nil {
			return fmt.Errorf("query duplicates: %w", err)
		}
		defer rows.Close()

		var sha string
		var cnt int
		found := false
		for rows.Next() {
			if err := rows.Scan(&sha, &cnt); err != nil {
				return err
			}
			found = true
			fmt.Printf("duplicate group (sha256=%s) — %d files:\n", sha, cnt)
			pr, err := db.Query(`SELECT path FROM images WHERE sha256 = ? ORDER BY path`, sha)
			if err != nil {
				log.Println("warning:", err)
				continue
			}
			for pr.Next() {
				var p string
				if err := pr.Scan(&p); err == nil {
					fmt.Printf("  %s\n", p)
				}
			}
			pr.Close()
		}
		if !found {
			fmt.Println("no exact duplicates found")
		}
		return nil
	},
}
