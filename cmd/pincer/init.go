package pincer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Pincer configuration",
	RunE:  runInit,
}

type providerOption struct {
	name     string
	model    string
	envVar   string
	envLabel string
}

type channelOption struct {
	name   string
	envVar string
}

var providers = []providerOption{
	{"Anthropic", "claude-sonnet-4-20250514", "ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY"},
	{"OpenAI", "gpt-4o", "OPENAI_API_KEY", "OPENAI_API_KEY"},
	{"Gemini", "gemini-2.0-flash", "GEMINI_API_KEY", "GEMINI_API_KEY"},
	{"Ollama (local)", "ollama/llama3", "", ""},
}

var channelOptions = []channelOption{
	{"telegram", "TELEGRAM_BOT_TOKEN"},
	{"discord", "DISCORD_BOT_TOKEN"},
	{"slack", "SLACK_BOT_TOKEN (+ SLACK_APP_TOKEN)"},
	{"whatsapp", "WHATSAPP_DB_PATH"},
	{"matrix", "MATRIX_HOMESERVER (+ MATRIX_USER_ID, MATRIX_TOKEN)"},
}

func runInit(cmd *cobra.Command, args []string) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("  Welcome to Pincer!")
	fmt.Println("  This wizard will create a configuration file to get you started.")
	fmt.Println()

	if err := config.EnsureDataDir(); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	configPath := filepath.Join(config.DataDir(), "pincer.toml")
	if _, err := os.Stat(configPath); err == nil {
		answer := prompt(scanner, fmt.Sprintf("  Config already exists at %s. Overwrite? [y/N]", configPath), "n")
		if strings.ToLower(answer) != "y" {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	fmt.Println("  1. LLM Provider")
	providerLabels := make([]string, len(providers))
	for i, p := range providers {
		providerLabels[i] = p.name
	}
	providerIdx := promptChoice(scanner, "  Which LLM provider?", providerLabels, 0)
	chosen := providers[providerIdx]

	fmt.Println()
	fmt.Println("  2. Channel Adapters")
	fmt.Println("  Channels let Pincer connect to messaging platforms.")
	channelLabels := make([]string, len(channelOptions))
	for i, c := range channelOptions {
		channelLabels[i] = c.name
	}
	selectedChannels := promptMultiChoice(scanner, "  Enable channels (comma-separated numbers, or Enter to skip):", channelLabels)

	fmt.Println()
	fmt.Println("  3. Tool Approval Mode")
	approvalIdx := promptChoice(scanner, "  How should tool calls be approved?", []string{
		"ask  - prompt before each tool call",
		"auto - always approve automatically",
		"deny - always deny tool calls",
	}, 0)
	approvalModes := []string{"ask", "auto", "deny"}
	approvalMode := approvalModes[approvalIdx]

	cfg := config.Default()
	cfg.Agent.Model = chosen.model
	cfg.Agent.ToolApproval = approvalMode

	if len(selectedChannels) > 0 {
		cfg.Channels = make(map[string]config.ChannelConfig)
		for _, idx := range selectedChannels {
			ch := channelOptions[idx]
			cfg.Channels[ch.name] = config.ChannelConfig{Enabled: true}
		}
	}

	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Println()
	fmt.Println("  Configuration saved to", configPath)
	fmt.Println()

	var envVars []string
	if chosen.envVar != "" {
		envVars = append(envVars, chosen.envLabel)
	}
	for _, idx := range selectedChannels {
		envVars = append(envVars, channelOptions[idx].envVar)
	}

	if len(envVars) > 0 {
		fmt.Println("  Set these environment variables before starting:")
		for _, v := range envVars {
			fmt.Printf("    export %s=<your-value>\n", v)
		}
		fmt.Println()
	}

	fmt.Println("  Start Pincer with:")
	fmt.Println("    pincer start")
	fmt.Println()

	return nil
}

func prompt(scanner *bufio.Scanner, question string, defaultVal string) string {
	fmt.Print(question + " ")
	if scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text != "" {
			return text
		}
	}
	return defaultVal
}

func promptChoice(scanner *bufio.Scanner, question string, options []string, defaultIdx int) int {
	fmt.Println(question)
	for i, opt := range options {
		marker := "  "
		if i == defaultIdx {
			marker = "* "
		}
		fmt.Printf("    %s%d) %s\n", marker, i+1, opt)
	}
	for {
		answer := prompt(scanner, fmt.Sprintf("  Choice [%d]:", defaultIdx+1), strconv.Itoa(defaultIdx+1))
		n, err := strconv.Atoi(answer)
		if err == nil && n >= 1 && n <= len(options) {
			return n - 1
		}
		fmt.Println("  Please enter a number between 1 and", len(options))
	}
}

func promptMultiChoice(scanner *bufio.Scanner, question string, options []string) []int {
	fmt.Println(question)
	for i, opt := range options {
		fmt.Printf("    %d) %s\n", i+1, opt)
	}
	answer := prompt(scanner, "  Selection:", "")
	if answer == "" {
		return nil
	}

	var selected []int
	seen := make(map[int]bool)
	for _, part := range strings.Split(answer, ",") {
		part = strings.TrimSpace(part)
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > len(options) {
			continue
		}
		idx := n - 1
		if !seen[idx] {
			seen[idx] = true
			selected = append(selected, idx)
		}
	}
	return selected
}
