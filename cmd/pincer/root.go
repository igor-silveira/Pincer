package pincer

import (
	"fmt"

	"github.com/spf13/cobra"
)

const version = "0.1.0"

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "pincer",
	Short: "Pincer - a secure, high-performance AI assistant gateway",
	Long:  "Pincer is a self-hosted AI assistant framework with multi-channel messaging, sandboxed tool execution, and encrypted credential storage.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.pincer/pincer.toml)")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(chatCmd)
	rootCmd.AddCommand(auditCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of Pincer",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("pincer v%s\n", version)
	},
}
