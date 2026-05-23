package cli

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/go-birds/photosift/internal/analyze"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

var (
	anNearDist   int
	anSubjectMin int
	anBlur       float64
)

func init() {
	rootCmd.AddCommand(analyzeCmd)
	analyzeCmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "path to sqlite db")
	analyzeCmd.Flags().IntVar(&anNearDist, "near-dist", 6, "max Hamming distance for near-duplicates")
	analyzeCmd.Flags().IntVar(&anSubjectMin, "subject-min", 5, "group size that counts as too many photos of one subject")
	analyzeCmd.Flags().Float64Var(&anBlur, "blur-threshold", 120, "sharpness below this is flagged blurry")
}

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Score indexed images and produce deletion suggestions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		start := time.Now()
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer db.Close()

		sum, err := analyze.Run(db, analyze.Options{
			NearDist:      anNearDist,
			SubjectMin:    anSubjectMin,
			BlurThreshold: anBlur,
		})
		if err != nil {
			return err
		}

		fmt.Printf("analyzed %d images in %s\n", sum.Total, time.Since(start))
		cats := make([]string, 0, len(sum.Counts))
		for c := range sum.Counts {
			cats = append(cats, c)
		}
		sort.Strings(cats)
		for _, c := range cats {
			fmt.Printf("  %-18s %d\n", c, sum.Counts[c])
		}
		fmt.Printf("%d distinct images suggested for deletion\n", sum.CandidateImages)
		fmt.Printf("run 'photosift serve' to review them\n")
		return nil
	},
}
