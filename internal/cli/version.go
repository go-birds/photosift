package cli

import (
	"encoding/json"
	"fmt"

	"github.com/go-birds/photosift/internal/buildinfo"
	"github.com/spf13/cobra"
)

var versionJSON bool

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolVar(&versionJSON, "json", false, "Output version information as JSON")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of PhotoSift",
	Run: func(cmd *cobra.Command, args []string) {
		if versionJSON {
			out := map[string]string{
				"version": buildinfo.Version,
				"go":      buildinfo.GoVersion(),
			}
			b, _ := json.Marshal(out)
			fmt.Println(string(b))
			return
		}

		fmt.Printf("photosift v%s\n", buildinfo.Version)

	},
}
