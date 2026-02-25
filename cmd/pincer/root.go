package pincer

import (
	"fmt"

	"github.com/spf13/cobra"
)

var version = "1.0.0"

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "pincer",
	Short: "Pincer - a self-hosted AI assistant gateway",
	Long: `Pincer is a self-hosted AI assistant framework that connects to messaging
platforms, runs agentic tool loops, and manages conversations with persistent
memory. Single binary, no CGo.

Get started:
  pincer init     Create a configuration file
  pincer start    Start the gateway
  pincer chat     Open the interactive TUI
  pincer doctor   Check your installation`,
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
	rootCmd.AddCommand(initCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of Pincer",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("pincer v%s\n", version)
	},
}
