package pincer

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/igorsilveira/pincer/pkg/config"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose issues with the Pincer installation",
	RunE:  runDoctor,
}

type checkResult struct {
	name   string
	ok     bool
	detail string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Printf("Pincer Doctor v%s\n", version)
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Go: %s\n\n", runtime.Version())

	checks := []checkResult{
		checkDataDir(),
		checkConfig(),
		checkDatabase(),
		checkAnthropicKey(),
		checkOpenAIKey(),
		checkGeminiKey(),
		checkDocker(),
		checkChrome(),
		checkGatewayHealth(),
	}

	passed, failed := 0, 0
	for _, c := range checks {
		status := "✓"
		if !c.ok {
			status = "✗"
			failed++
		} else {
			passed++
		}
		fmt.Printf("  %s %s: %s\n", status, c.name, c.detail)
	}

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)

	if failed > 0 {
		return fmt.Errorf("%d checks failed", failed)
	}
	return nil
}

func checkDataDir() checkResult {
	dir := config.DataDir()
	info, err := os.Stat(dir)
	if err != nil {
		return checkResult{"Data directory", false, fmt.Sprintf("%s does not exist", dir)}
	}
	if !info.IsDir() {
		return checkResult{"Data directory", false, fmt.Sprintf("%s is not a directory", dir)}
	}
	return checkResult{"Data directory", true, dir}
}

func checkConfig() checkResult {
	path := config.DefaultConfigPath()
	if _, err := os.Stat(path); err != nil {
		return checkResult{"Config file", false, fmt.Sprintf("%s not found (using defaults)", path)}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return checkResult{"Config file", false, fmt.Sprintf("parse error: %s", err)}
	}
	return checkResult{"Config file", true, fmt.Sprintf("%s (port %d)", path, cfg.Gateway.Port)}
}

func checkDatabase() checkResult {
	cfg := config.Current()
	dsn := cfg.Store.DSN
	if dsn == "" {
		dsn = filepath.Join(config.DataDir(), "pincer.db")
	}
	if _, err := os.Stat(dsn); err != nil {
		return checkResult{"Database", false, fmt.Sprintf("%s not found (will be created on first start)", dsn)}
	}
	info, _ := os.Stat(dsn)
	return checkResult{"Database", true, fmt.Sprintf("%s (%d KB)", dsn, info.Size()/1024)}
}

func checkAnthropicKey() checkResult {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return checkResult{"Anthropic API key", false, "ANTHROPIC_API_KEY not set"}
	}
	return checkResult{"Anthropic API key", true, fmt.Sprintf("set (%d chars)", len(key))}
}

func checkOpenAIKey() checkResult {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		return checkResult{"OpenAI API key", false, "OPENAI_API_KEY not set (optional)"}
	}
	return checkResult{"OpenAI API key", true, fmt.Sprintf("set (%d chars)", len(key))}
}

func checkGeminiKey() checkResult {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		return checkResult{"Gemini API key", false, "GEMINI_API_KEY not set (optional)"}
	}
	return checkResult{"Gemini API key", true, fmt.Sprintf("set (%d chars)", len(key))}
}

func checkDocker() checkResult {
	for _, rt := range []string{"docker", "podman", "nerdctl"} {
		if path, err := exec.LookPath(rt); err == nil {
			return checkResult{"Container runtime", true, fmt.Sprintf("%s at %s", rt, path)}
		}
	}
	return checkResult{"Container runtime", false, "no container runtime found (optional, for container sandbox)"}
}

func checkChrome() checkResult {
	paths := []string{"google-chrome", "chromium-browser", "chromium"}
	if runtime.GOOS == "darwin" {
		paths = append(paths, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
	}
	for _, p := range paths {
		if _, err := exec.LookPath(p); err == nil {
			return checkResult{"Chrome/Chromium", true, p}
		}
	}

	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/Google Chrome.app"); err == nil {
			return checkResult{"Chrome/Chromium", true, "/Applications/Google Chrome.app"}
		}
	}
	return checkResult{"Chrome/Chromium", false, "not found (optional, for browser tool)"}
}

func checkGatewayHealth() checkResult {
	cfg := config.Current()
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Gateway.Port)

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return checkResult{"Gateway", false, "not running"}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return checkResult{"Gateway", true, fmt.Sprintf("running at :%d", cfg.Gateway.Port)}
	}
	return checkResult{"Gateway", false, fmt.Sprintf("unhealthy (status %d)", resp.StatusCode)}
}
