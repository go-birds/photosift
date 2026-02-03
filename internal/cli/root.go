// root.go

package cli

import (
	"fmt"
	"os"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use: "photosift",
	Short: "photosift - local visual similarity and dupe finder",
}

func Execute() {
	if err:= rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
