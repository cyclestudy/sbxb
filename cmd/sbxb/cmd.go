package sbxb

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "sbxb",
	Short: "sing-box node backend for XBoard",
	Run: func(cmd *cobra.Command, args []string) {
		showMenu()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
