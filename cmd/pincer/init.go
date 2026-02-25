package pincer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/igorsilveira/pincer/pkg/llm"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a new Pincer configuration interactively",
	Long: `Run the interactive setup wizard to configure your LLM provider,
messaging channels, and tool approval policy. Detects existing API keys
and tokens from environment variables.`,
	Example: "  pincer init",
	RunE:    runInit,
}

type providerOption struct {
	name   string
	model  string
	envVar string
}

type channelOption struct {
	name    string
	envVars []string
}

type envDetection struct {
	name   string
	envVar string
	set    bool
}

var providers = []providerOption{
	{"Anthropic", "claude-sonnet-4-20250514", "ANTHROPIC_API_KEY"},
	{"OpenAI", "gpt-4o", "OPENAI_API_KEY"},
	{"Gemini", "gemini-2.0-flash", "GEMINI_API_KEY"},
	{"Ollama (local)", "ollama/llama3", ""},
}

var channelOptions = []channelOption{
	{"telegram", []string{"TELEGRAM_BOT_TOKEN"}},
	{"discord", []string{"DISCORD_BOT_TOKEN"}},
	{"slack", []string{"SLACK_BOT_TOKEN", "SLACK_APP_TOKEN"}},
	{"whatsapp", []string{"WHATSAPP_DB_PATH"}},
	{"matrix", []string{"MATRIX_HOMESERVER", "MATRIX_USER_ID", "MATRIX_TOKEN"}},
}

func runInit(cmd *cobra.Command, args []string) error {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	subtitleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fmt.Println()
	fmt.Println(titleStyle.Render("  Welcome to Pincer"))
	fmt.Println(subtitleStyle.Render("  Interactive setup wizard"))
	fmt.Println()

	if err := config.EnsureDataDir(); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	configPath := filepath.Join(config.DataDir(), "pincer.toml")
	if _, err := os.Stat(configPath); err == nil {
		var overwrite bool
		err := huh.NewConfirm().
			Title("Config already exists at " + configPath).
			Description("Do you want to overwrite it?").
			Affirmative("Overwrite").
			Negative("Cancel").
			Value(&overwrite).
			Run()
		if err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("  Setup cancelled.")
				return nil
			}
			return fmt.Errorf("running confirm: %w", err)
		}
		if !overwrite {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	detections := detectEnvVars()
	printDetections(detections)

	var providerChoice int
	var selectedChannels []string
	var approvalMode string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("LLM Provider").
				Description("Which provider do you want to use?").
				Options(buildProviderOptions(detections)...).
				Value(&providerChoice),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Channel Adapters").
				Description("Select messaging platforms to enable.").
				Options(buildChannelOptions(detections)...).
				Value(&selectedChannels),
		),
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Tool Approval Mode").
				Description("How should tool calls be approved?").
				Options(
					huh.NewOption("Ask - prompt before each tool call (recommended)", "ask"),
					huh.NewOption("Auto - always approve automatically", "auto"),
					huh.NewOption("Deny - always deny tool calls", "deny"),
				).
				Value(&approvalMode),
		),
	).WithTheme(huh.ThemeCharm())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			fmt.Println("  Setup cancelled.")
			return nil
		}
		return fmt.Errorf("running setup form: %w", err)
	}

	chosen := providers[providerChoice]

	cfg := config.Default()
	cfg.Agent.Model = chosen.model
	cfg.Agent.ToolApproval = approvalMode

	if len(selectedChannels) > 0 {
		cfg.Channels = make(map[string]config.ChannelConfig)
		for _, ch := range selectedChannels {
			cfg.Channels[ch] = config.ChannelConfig{Enabled: true}
		}
	}

	if chosen.envVar != "" && envVarSet(detections, chosen.envVar) {
		testConnectivity(chosen)
	}

	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	neededVars := missingEnvVars(chosen, selectedChannels, detections)
	printSummary(configPath, chosen, selectedChannels, neededVars)

	return nil
}

func detectEnvVars() []envDetection {
	vars := []struct {
		name   string
		envVar string
	}{
		{"Anthropic API Key", "ANTHROPIC_API_KEY"},
		{"OpenAI API Key", "OPENAI_API_KEY"},
		{"Gemini API Key", "GEMINI_API_KEY"},
		{"Telegram Bot Token", "TELEGRAM_BOT_TOKEN"},
		{"Discord Bot Token", "DISCORD_BOT_TOKEN"},
		{"Slack Bot Token", "SLACK_BOT_TOKEN"},
		{"Slack App Token", "SLACK_APP_TOKEN"},
		{"WhatsApp DB Path", "WHATSAPP_DB_PATH"},
		{"Matrix Homeserver", "MATRIX_HOMESERVER"},
		{"Matrix User ID", "MATRIX_USER_ID"},
		{"Matrix Token", "MATRIX_TOKEN"},
	}

	results := make([]envDetection, 0, len(vars))
	for _, v := range vars {
		results = append(results, envDetection{
			name:   v.name,
			envVar: v.envVar,
			set:    os.Getenv(v.envVar) != "",
		})
	}
	return results
}

func printDetections(detections []envDetection) {
	checkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var found []string
	for _, d := range detections {
		if d.set {
			found = append(found, d.envVar)
		}
	}

	if len(found) > 0 {
		fmt.Println(checkStyle.Render("  Detected: ") + dimStyle.Render(strings.Join(found, ", ")))
		fmt.Println()
	}
}

func envVarSet(detections []envDetection, envVar string) bool {
	for _, d := range detections {
		if d.envVar == envVar && d.set {
			return true
		}
	}
	return false
}

func buildProviderOptions(detections []envDetection) []huh.Option[int] {
	opts := make([]huh.Option[int], len(providers))
	for i, p := range providers {
		label := fmt.Sprintf("%s (%s)", p.name, p.model)
		if p.envVar != "" && envVarSet(detections, p.envVar) {
			label += " ✓ key detected"
		}
		opts[i] = huh.NewOption(label, i)
	}
	return opts
}

func buildChannelOptions(detections []envDetection) []huh.Option[string] {
	opts := make([]huh.Option[string], len(channelOptions))
	for i, ch := range channelOptions {
		label := ch.name
		allSet := true
		for _, ev := range ch.envVars {
			if !envVarSet(detections, ev) {
				allSet = false
				break
			}
		}
		if allSet {
			label += " ✓ configured"
		}
		opt := huh.NewOption(label, ch.name)
		if allSet {
			opt = opt.Selected(true)
		}
		opts[i] = opt
	}
	return opts
}

func testConnectivity(chosen providerOption) {
	checkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	fmt.Printf("  Testing %s connectivity... ", chosen.name)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if strings.HasPrefix(chosen.model, "ollama/") {
		baseURL := os.Getenv("OLLAMA_BASE_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(baseURL)
		if err != nil {
			fmt.Println(warnStyle.Render("✗ " + err.Error()))
			return
		}
		resp.Body.Close()
		fmt.Println(checkStyle.Render("✓ connected"))
		return
	}

	var provider llm.Provider
	var err error

	switch chosen.envVar {
	case "ANTHROPIC_API_KEY":
		provider, err = llm.NewAnthropicProvider("", "")
	case "OPENAI_API_KEY":
		provider, err = llm.NewOpenAIProvider("", "")
	case "GEMINI_API_KEY":
		provider, err = llm.NewGeminiProvider("")
	}
	if err != nil {
		fmt.Println(warnStyle.Render("✗ " + err.Error()))
		return
	}

	ch, err := provider.Chat(ctx, llm.ChatRequest{
		Model:     chosen.model,
		Messages:  make([]llm.ChatMessage, 0),
		MaxTokens: 1,
		Stream:    true,
	})
	if err != nil {
		fmt.Println(warnStyle.Render("✗ " + err.Error()))
		return
	}

	for ev := range ch {
		if ev.Type == llm.EventError {
			fmt.Println(warnStyle.Render("✗ " + ev.Error.Error()))
			return
		}
	}

	fmt.Println(checkStyle.Render("✓ connected"))
}

func missingEnvVars(chosen providerOption, selectedChannels []string, detections []envDetection) []string {
	var needed []string

	if chosen.envVar != "" && !envVarSet(detections, chosen.envVar) {
		needed = append(needed, chosen.envVar)
	}

	for _, chName := range selectedChannels {
		for _, ch := range channelOptions {
			if ch.name != chName {
				continue
			}
			for _, ev := range ch.envVars {
				if !envVarSet(detections, ev) {
					needed = append(needed, ev)
				}
			}
		}
	}

	return needed
}

func printSummary(configPath string, chosen providerOption, channels []string, neededVars []string) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	checkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fmt.Println()
	fmt.Println(titleStyle.Render("  Setup Complete"))
	fmt.Println()
	fmt.Println(checkStyle.Render("  ✓") + " Config saved to " + configPath)
	fmt.Println(checkStyle.Render("  ✓") + " Provider: " + chosen.name)
	if len(channels) > 0 {
		fmt.Println(checkStyle.Render("  ✓") + " Channels: " + strings.Join(channels, ", "))
	} else {
		fmt.Println(dimStyle.Render("  - No channels enabled (webchat only)"))
	}

	if len(neededVars) > 0 {
		fmt.Println()
		fmt.Println(warnStyle.Render("  Set these environment variables before starting:"))
		fmt.Println()
		for _, v := range neededVars {
			fmt.Printf("    export %s=<your-value>\n", v)
		}
	}

	fmt.Println()
	fmt.Println("  Start with: " + titleStyle.Render("pincer start"))
	fmt.Println()
}
