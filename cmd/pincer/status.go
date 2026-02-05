package pincer

import (
	"fmt"
	"net/http"
	"time"

	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the health of the Pincer gateway",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	cfg := config.Current()
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Gateway.Port)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("status: gateway is not running")
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("status: gateway is healthy")
	} else {
		fmt.Printf("status: gateway returned %s\n", resp.Status)
	}
	return nil
}
