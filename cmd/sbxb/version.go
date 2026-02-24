package sbxb

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("sbxb %s (%s)\n", version, commit)
		fmt.Printf("Go %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	},
}
