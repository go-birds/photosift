package cli

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-birds/photosift/internal/analyze"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

var (
	mlModel  string
	mlLabels string
	mlLib    string
)

func init() {
	rootCmd.AddCommand(mlCmd)
	mlCmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "path to sqlite db")
	mlCmd.Flags().StringVar(&mlModel, "model", "", "path to .onnx model (auto-downloaded to ~/.photosift/models if absent)")
	mlCmd.Flags().StringVar(&mlLabels, "labels", "", "path to labels JSON (auto-downloaded if absent)")
	mlCmd.Flags().StringVar(&mlLib, "onnx-lib", "", "path to libonnxruntime.so/.dylib (or set PHOTOSIFT_ONNX_LIB)")
}

var mlCmd = &cobra.Command{
	Use:   "ml",
	Short: "Run the bundled MobileNetV2 classifier to label images (Go-native ML, requires -tags onnx build)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open db: %w", err)
		}
		defer db.Close()

		n, err := analyze.ClassifyWithONNX(db, analyze.ONNXOptions{
			ModelPath:  mlModel,
			LabelsPath: mlLabels,
			OnnxLib:    mlLib,
		})
		if errors.Is(err, analyze.ErrONNXNotCompiled) {
			return err
		}
		if err != nil {
			return err
		}
		fmt.Printf("labelled %d images — re-run 'photosift analyze' to use them\n", n)
		return nil
	},
}
