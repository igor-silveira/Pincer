package skills

import (
	"fmt"
	"regexp"
	"strings"
)

type Finding struct {
	Rule    string
	Message string
}

type ScanResult struct {
	SkillName string
	Findings  []Finding
	Safe      bool
}

func Scan(sk *Skill) ScanResult {
	result := ScanResult{SkillName: sk.Name, Safe: true}

	scanText(&result, sk.Prompt, "prompt")

	for _, t := range sk.Tools {
		scanText(&result, t.Description, fmt.Sprintf("tool[%s].description", t.Name))
		scanText(&result, string(t.InputSchema), fmt.Sprintf("tool[%s].schema", t.Name))
	}

	checkPolicy(&result, sk)

	result.Safe = len(result.Findings) == 0
	return result
}

var exfilPatterns = []struct {
	name    string
	pattern *regexp.Regexp
	message string
}{
	{"curl_exfil", regexp.MustCompile(`(?i)\bcurl\b.*-[dX]`), "curl with data flag may exfiltrate data"},
	{"wget_post", regexp.MustCompile(`(?i)\bwget\b.*--post`), "wget with POST may exfiltrate data"},
	{"nc_reverse", regexp.MustCompile(`(?i)\b(nc|ncat|netcat)\b.*-[el]`), "netcat listener may open reverse shell"},
	{"base64_url", regexp.MustCompile(`(?i)base64.*https?://`), "base64 encoded URL detected"},
	{"encoded_url", regexp.MustCompile(`aHR0c[A-Za-z0-9+/=]+`), "base64 encoded HTTP URL detected"},
	{"eval_exec", regexp.MustCompile(`(?i)\b(eval|exec)\b.*\$`), "dynamic code execution with variable expansion"},
	{"env_leak", regexp.MustCompile(`(?i)\$\{?(API_KEY|SECRET|TOKEN|PASSWORD|CREDENTIALS)`), "potential secret/credential access"},
	{"pipe_remote", regexp.MustCompile(`(?i)\|\s*(curl|wget|nc)`), "piping output to network command"},
	{"dns_exfil", regexp.MustCompile(`(?i)\bdig\b.*@`), "DNS query to custom server may exfiltrate data"},
}

func scanText(result *ScanResult, text, location string) {
	if text == "" {
		return
	}

	for _, p := range exfilPatterns {
		if p.pattern.MatchString(text) {
			result.Findings = append(result.Findings, Finding{
				Rule:    p.name,
				Message: fmt.Sprintf("[%s] %s", location, p.message),
			})
		}
	}

	if strings.Contains(text, "#!/") {
		result.Findings = append(result.Findings, Finding{
			Rule:    "inline_script",
			Message: fmt.Sprintf("[%s] inline script shebang detected", location),
		})
	}
}

func checkPolicy(result *ScanResult, sk *Skill) {
	hasNetworkTool := false
	hasShellTool := false
	hasFSTool := false

	for _, t := range sk.Tools {
		name := strings.ToLower(t.Name)
		desc := strings.ToLower(t.Description)
		if strings.Contains(name, "http") || strings.Contains(desc, "network") || strings.Contains(desc, "http") {
			hasNetworkTool = true
		}
		if strings.Contains(name, "shell") || strings.Contains(name, "exec") || strings.Contains(desc, "command") {
			hasShellTool = true
		}
		if strings.Contains(name, "file") || strings.Contains(desc, "filesystem") || strings.Contains(desc, "read") || strings.Contains(desc, "write") {
			hasFSTool = true
		}
	}

	if hasNetworkTool && !sk.Policy.Network {
		result.Findings = append(result.Findings, Finding{
			Rule:    "undeclared_network",
			Message: "skill has network-capable tools but does not declare network policy",
		})
	}
	if hasShellTool && !sk.Policy.Shell {
		result.Findings = append(result.Findings, Finding{
			Rule:    "undeclared_shell",
			Message: "skill has shell-capable tools but does not declare shell policy",
		})
	}
	if hasFSTool && !sk.Policy.Filesystem {
		result.Findings = append(result.Findings, Finding{
			Rule:    "undeclared_filesystem",
			Message: "skill has filesystem-capable tools but does not declare filesystem policy",
		})
	}
}
