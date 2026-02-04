package pincer

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pincer",
	Short: "Pincer - a secure, high-performance AI assistant gateway",
	Long:  "Pincer is a self-hosted AI assistant framework with multi-channel messaging, sandboxed tool execution, and encrypted credential storage.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of Pincer",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("pincer v0.1.0")
	},
}
