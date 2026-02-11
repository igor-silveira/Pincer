package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	inputStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type Message struct {
	Role    string
	Content string
}

type SendFunc func(input string) (string, error)

type Model struct {
	messages []Message
	input    string
	sendFn   SendFunc
	width    int
	height   int
	scroll   int
	waiting  bool
	err      error
}

func NewModel(sendFn SendFunc) Model {
	return Model{
		sendFn: sendFn,
	}
}

type responseMsg struct {
	content string
	err     error
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if m.waiting || strings.TrimSpace(m.input) == "" {
				return m, nil
			}
			return m.submitInput()
		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case "pgup":
			if m.scroll > 0 {
				m.scroll--
			}
		case "pgdown":
			m.scroll++
		default:
			if len(msg.String()) == 1 || msg.String() == " " {
				m.input += msg.String()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case responseMsg:
		m.waiting = false
		if msg.err != nil {
			m.err = msg.err
			m.messages = append(m.messages, Message{
				Role:    "error",
				Content: msg.err.Error(),
			})
		} else {
			m.messages = append(m.messages, Message{
				Role:    "assistant",
				Content: msg.content,
			})
		}
	}

	return m, nil
}

func (m Model) submitInput() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input)
	m.messages = append(m.messages, Message{Role: "user", Content: text})
	m.input = ""
	m.waiting = true

	sendFn := m.sendFn
	return m, func() tea.Msg {
		resp, err := sendFn(text)
		return responseMsg{content: resp, err: err}
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	header := dimStyle.Render("Pincer Chat (Ctrl+C to quit)")
	b.WriteString(header)
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n\n")

	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			b.WriteString(userStyle.Render("You: "))
			b.WriteString(msg.Content)
		case "assistant":
			b.WriteString(assistantStyle.Render("Pincer: "))
			b.WriteString(msg.Content)
		case "error":
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Error: "))
			b.WriteString(msg.Content)
		}
		b.WriteString("\n\n")
	}

	if m.waiting {
		b.WriteString(dimStyle.Render("Thinking..."))
		b.WriteString("\n\n")
	}

	b.WriteString(dimStyle.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")
	prompt := inputStyle.Render("> " + m.input)
	if !m.waiting {
		prompt += dimStyle.Render("█")
	}
	b.WriteString(prompt)

	return b.String()
}

func Run(sendFn SendFunc) error {
	p := tea.NewProgram(NewModel(sendFn), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func RunWithAddress(addr string) error {
	return Run(func(input string) (string, error) {
		return "", fmt.Errorf("direct gateway connection not yet implemented (use WebSocket at %s/ws)", addr)
	})
}
