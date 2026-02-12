package pincer

import (
	"fmt"

	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/igorsilveira/pincer/pkg/tui"
	"github.com/spf13/cobra"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive TUI chat session",
	RunE:  runChat,
}

func runChat(cmd *cobra.Command, args []string) error {
	cfg := config.Current()
	addr := fmt.Sprintf("http://127.0.0.1:%d", cfg.Gateway.Port)
	return tui.RunWithAddress(addr)
}
