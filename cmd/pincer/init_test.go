package pincer

import (
	"testing"
)

func TestDetectEnvVars(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-123")
	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token-456")

	detections := detectEnvVars()

	want := map[string]bool{
		"ANTHROPIC_API_KEY":  true,
		"OPENAI_API_KEY":     false,
		"GEMINI_API_KEY":     false,
		"TELEGRAM_BOT_TOKEN": true,
		"DISCORD_BOT_TOKEN":  false,
		"SLACK_BOT_TOKEN":    false,
		"SLACK_APP_TOKEN":    false,
		"WHATSAPP_DB_PATH":   false,
		"MATRIX_HOMESERVER":  false,
		"MATRIX_USER_ID":     false,
		"MATRIX_TOKEN":       false,
	}

	for _, d := range detections {
		expected, ok := want[d.envVar]
		if !ok {
			t.Errorf("unexpected env var in detections: %s", d.envVar)
			continue
		}
		if d.set != expected {
			t.Errorf("detectEnvVars() %s: got set=%v, want %v", d.envVar, d.set, expected)
		}
	}

	if len(detections) != len(want) {
		t.Errorf("detectEnvVars() returned %d results, want %d", len(detections), len(want))
	}
}

func TestEnvVarSet(t *testing.T) {
	detections := []envDetection{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", set: true},
		{name: "OpenAI", envVar: "OPENAI_API_KEY", set: false},
	}

	if !envVarSet(detections, "ANTHROPIC_API_KEY") {
		t.Error("envVarSet() should return true for ANTHROPIC_API_KEY")
	}
	if envVarSet(detections, "OPENAI_API_KEY") {
		t.Error("envVarSet() should return false for OPENAI_API_KEY")
	}
	if envVarSet(detections, "NONEXISTENT") {
		t.Error("envVarSet() should return false for unknown var")
	}
}

func TestBuildProviderOptions(t *testing.T) {
	detections := []envDetection{
		{name: "Anthropic API Key", envVar: "ANTHROPIC_API_KEY", set: true},
		{name: "OpenAI API Key", envVar: "OPENAI_API_KEY", set: false},
		{name: "Gemini API Key", envVar: "GEMINI_API_KEY", set: false},
	}

	opts := buildProviderOptions(detections)

	if len(opts) != len(providers) {
		t.Fatalf("buildProviderOptions() returned %d options, want %d", len(opts), len(providers))
	}

	if opts[0].Key == "" {
		t.Error("first option should have a non-empty key")
	}

	if opts[0].Value != 0 {
		t.Errorf("first option value: got %d, want 0", opts[0].Value)
	}

	for i, opt := range opts {
		if opt.Value != i {
			t.Errorf("option %d value: got %d, want %d", i, opt.Value, i)
		}
	}

	anthLabel := opts[0].Key
	if !contains(anthLabel, "key detected") {
		t.Errorf("Anthropic option should contain 'key detected', got %q", anthLabel)
	}

	openaiLabel := opts[1].Key
	if contains(openaiLabel, "key detected") {
		t.Errorf("OpenAI option should not contain 'key detected', got %q", openaiLabel)
	}
}

func TestBuildChannelOptions(t *testing.T) {
	detections := []envDetection{
		{name: "Telegram Bot Token", envVar: "TELEGRAM_BOT_TOKEN", set: true},
		{name: "Discord Bot Token", envVar: "DISCORD_BOT_TOKEN", set: false},
		{name: "Slack Bot Token", envVar: "SLACK_BOT_TOKEN", set: true},
		{name: "Slack App Token", envVar: "SLACK_APP_TOKEN", set: true},
		{name: "WhatsApp DB Path", envVar: "WHATSAPP_DB_PATH", set: false},
		{name: "Matrix Homeserver", envVar: "MATRIX_HOMESERVER", set: false},
		{name: "Matrix User ID", envVar: "MATRIX_USER_ID", set: false},
		{name: "Matrix Token", envVar: "MATRIX_TOKEN", set: false},
	}

	opts := buildChannelOptions(detections)

	if len(opts) != len(channelOptions) {
		t.Fatalf("buildChannelOptions() returned %d options, want %d", len(opts), len(channelOptions))
	}

	telegramLabel := opts[0].Key
	if !contains(telegramLabel, "configured") {
		t.Errorf("Telegram option should contain 'configured', got %q", telegramLabel)
	}

	discordLabel := opts[1].Key
	if contains(discordLabel, "configured") {
		t.Errorf("Discord option should not contain 'configured', got %q", discordLabel)
	}

	slackLabel := opts[2].Key
	if !contains(slackLabel, "configured") {
		t.Errorf("Slack option should contain 'configured' (both tokens set), got %q", slackLabel)
	}
}

func TestMissingEnvVars(t *testing.T) {
	detections := []envDetection{
		{envVar: "ANTHROPIC_API_KEY", set: true},
		{envVar: "OPENAI_API_KEY", set: false},
		{envVar: "TELEGRAM_BOT_TOKEN", set: true},
		{envVar: "DISCORD_BOT_TOKEN", set: false},
		{envVar: "SLACK_BOT_TOKEN", set: false},
		{envVar: "SLACK_APP_TOKEN", set: false},
	}

	chosen := providers[0]
	channels := []string{"telegram", "discord"}

	needed := missingEnvVars(chosen, channels, detections)

	if len(needed) != 1 {
		t.Fatalf("missingEnvVars() returned %d vars, want 1: %v", len(needed), needed)
	}
	if needed[0] != "DISCORD_BOT_TOKEN" {
		t.Errorf("missingEnvVars()[0] = %q, want DISCORD_BOT_TOKEN", needed[0])
	}
}

func TestMissingEnvVarsIncludesProvider(t *testing.T) {
	detections := []envDetection{
		{envVar: "OPENAI_API_KEY", set: false},
	}

	chosen := providers[1]

	needed := missingEnvVars(chosen, nil, detections)

	if len(needed) != 1 || needed[0] != "OPENAI_API_KEY" {
		t.Errorf("missingEnvVars() = %v, want [OPENAI_API_KEY]", needed)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
