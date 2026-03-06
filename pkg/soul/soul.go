package soul

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/igorsilveira/pincer/pkg/memory"
)

type Soul struct {
	Identity    Identity     `toml:"identity"`
	Values      Values       `toml:"values"`
	Tone        Tone         `toml:"tone"`
	Boundaries  Boundaries   `toml:"boundaries"`
	Expertise   Expertise    `toml:"expertise"`
	MemorySeeds []MemorySeed `toml:"memory_seeds"`
}

type Identity struct {
	Name        string   `toml:"name"`
	Role        string   `toml:"role"`
	Personality []string `toml:"personality"`
}

type Values struct {
	Core       []string `toml:"core"`
	Priorities string   `toml:"priorities"`
}

type Tone struct {
	Style      string   `toml:"style"`
	Verbosity  string   `toml:"verbosity"`
	Guidelines []string `toml:"guidelines"`
}

type Boundaries struct {
	Refuse           []string `toml:"refuse"`
	DisclaimerTopics []string `toml:"disclaimer_topics"`
}

type Expertise struct {
	Domains []string `toml:"domains"`
}

type MemorySeed struct {
	Key   string `toml:"key"`
	Value string `toml:"value"`
}

func Load(path string) (*Soul, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("soul: reading %s: %w", path, err)
	}

	s := Default()
	if err := toml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("soul: parsing %s: %w", path, err)
	}

	return s, nil
}

func Default() *Soul {
	return &Soul{
		Identity: Identity{
			Name:        "Pincer",
			Role:        "AI assistant",
			Personality: []string{"helpful", "precise", "thoughtful"},
		},
		Values: Values{
			Core:       []string{"honesty", "helpfulness", "safety"},
			Priorities: "accuracy over speed, clarity over brevity",
		},
		Tone: Tone{
			Style:     "conversational but professional",
			Verbosity: "concise",
		},
		Boundaries: Boundaries{
			Refuse:           []string{"generating malware", "impersonating real people"},
			DisclaimerTopics: []string{"medical", "legal", "financial"},
		},
		Expertise: Expertise{
			Domains: []string{"general knowledge"},
		},
	}
}

const operationalGuidelines = `
# Operational Guidelines

## Task Execution
- Complete multi-step tasks in a single turn by chaining tool calls. Do not stop to ask for confirmation between steps.
- If a tool call fails, try an alternative approach before giving up.

## Tool Selection
- For web pages: prefer browser over http_request. The browser renders JavaScript, captures screenshots, and supports interaction. Use http_request only for API calls or raw content downloads.
- For persistent information: use memory to store facts, user preferences, and project context that should survive across sessions.
- For secrets and API keys: use credential to store and retrieve encrypted secrets. Never store secrets in memory.
- For complex tasks: use subagent to delegate focused subtasks synchronously. Use spawn for independent background tasks when the user does not need to wait.

## Error Recovery
- When you see [System: ...] messages, the system encountered an error on your behalf. Reduce complexity: use fewer parallel tool calls, produce shorter responses, or break the task into smaller steps. Do not repeat the exact same approach.
- If a tool times out, retry with a simpler approach or different parameters.

## Context Awareness
- Long conversations are automatically summarized. Key information may be in a [Session Summary] at the start of your history.
- Store important facts in memory early to avoid losing them during summarization.`

func (s *Soul) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are %s, a %s.\n", s.Identity.Name, s.Identity.Role)

	if len(s.Identity.Personality) > 0 {
		fmt.Fprintf(&b, "Personality: %s.\n", strings.Join(s.Identity.Personality, ", "))
	}

	if len(s.Values.Core) > 0 {
		fmt.Fprintf(&b, "Core values: %s.\n", strings.Join(s.Values.Core, ", "))
	}
	if s.Values.Priorities != "" {
		fmt.Fprintf(&b, "Priorities: %s.\n", s.Values.Priorities)
	}

	if s.Tone.Style != "" {
		fmt.Fprintf(&b, "Communication style: %s.\n", s.Tone.Style)
	}
	if s.Tone.Verbosity != "" {
		fmt.Fprintf(&b, "Verbosity: %s.\n", s.Tone.Verbosity)
	}
	if len(s.Tone.Guidelines) > 0 {
		b.WriteString("\n## Communication Guidelines\n")
		for _, g := range s.Tone.Guidelines {
			fmt.Fprintf(&b, "- %s\n", g)
		}
	}

	if len(s.Expertise.Domains) > 0 {
		fmt.Fprintf(&b, "Areas of expertise: %s.\n", strings.Join(s.Expertise.Domains, ", "))
	}

	if len(s.Boundaries.Refuse) > 0 {
		fmt.Fprintf(&b, "Always refuse to: %s.\n", strings.Join(s.Boundaries.Refuse, "; "))
	}
	if len(s.Boundaries.DisclaimerTopics) > 0 {
		fmt.Fprintf(&b, "Add disclaimers when discussing: %s.\n", strings.Join(s.Boundaries.DisclaimerTopics, ", "))
	}

	b.WriteString(operationalGuidelines)
	b.WriteByte('\n')

	return b.String()
}

func (s *Soul) SeedMemory(ctx context.Context, mem *memory.Store, agentID string) error {
	for _, seed := range s.MemorySeeds {
		if err := mem.Set(ctx, agentID, seed.Key, seed.Value); err != nil {
			if strings.Contains(err.Error(), "immutable") {
				continue
			}
			return fmt.Errorf("soul: seeding memory key %q: %w", seed.Key, err)
		}
	}
	return nil
}

func (s *Soul) Section(name string) string {
	switch strings.ToLower(name) {
	case "identity":
		parts := []string{fmt.Sprintf("Name: %s", s.Identity.Name), fmt.Sprintf("Role: %s", s.Identity.Role)}
		if len(s.Identity.Personality) > 0 {
			parts = append(parts, fmt.Sprintf("Personality: %s", strings.Join(s.Identity.Personality, ", ")))
		}
		return strings.Join(parts, "\n")
	case "values":
		parts := []string{fmt.Sprintf("Core: %s", strings.Join(s.Values.Core, ", "))}
		if s.Values.Priorities != "" {
			parts = append(parts, fmt.Sprintf("Priorities: %s", s.Values.Priorities))
		}
		return strings.Join(parts, "\n")
	case "tone":
		parts := []string{}
		if s.Tone.Style != "" {
			parts = append(parts, fmt.Sprintf("Style: %s", s.Tone.Style))
		}
		if s.Tone.Verbosity != "" {
			parts = append(parts, fmt.Sprintf("Verbosity: %s", s.Tone.Verbosity))
		}
		for _, g := range s.Tone.Guidelines {
			parts = append(parts, fmt.Sprintf("- %s", g))
		}
		return strings.Join(parts, "\n")
	case "boundaries":
		parts := []string{}
		if len(s.Boundaries.Refuse) > 0 {
			parts = append(parts, fmt.Sprintf("Refuse: %s", strings.Join(s.Boundaries.Refuse, "; ")))
		}
		if len(s.Boundaries.DisclaimerTopics) > 0 {
			parts = append(parts, fmt.Sprintf("Disclaimer topics: %s", strings.Join(s.Boundaries.DisclaimerTopics, ", ")))
		}
		return strings.Join(parts, "\n")
	case "expertise":
		return fmt.Sprintf("Domains: %s", strings.Join(s.Expertise.Domains, ", "))
	default:
		return s.Render()
	}
}
