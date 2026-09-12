package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/utahta/herdr-codex-confirm/internal/confirm"
)

const (
	denyAction = iota
	feedbackAction
	approveAction
)

const maxFeedbackChars = 4096

var actionLabels = [...]string{"Deny", "Deny with feedback", "Approve once"}

type model struct {
	request       confirm.Request
	viewport      viewport.Model
	feedback      textarea.Model
	result        confirm.Result
	width, height int
	selected      int
	editing, done bool
}

var (
	headingStyle = lipgloss.NewStyle().Bold(true)
	detailStyle  = lipgloss.NewStyle().Faint(true)
	commandStyle = lipgloss.NewStyle().Bold(true).PaddingLeft(1).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("12"))
	buttonStyle         = lipgloss.NewStyle().Padding(0, 1)
	selectedButtonStyle = buttonStyle.Bold(true).Reverse(true)
)

func newModel(req confirm.Request) model {
	input := textarea.New()
	input.Placeholder = "Describe what Codex should change..."
	input.ShowLineNumbers = false
	input.Prompt = "│ "
	input.CharLimit = 0
	input.MaxHeight = 0
	return model{request: req, viewport: viewport.New(0, 0), feedback: input}
}

func Show(ctx context.Context, req confirm.Request) (confirm.Result, error) {
	m := newModel(req)
	result, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithoutSignalHandler()).Run()
	if err != nil {
		return confirm.Result{}, err
	}
	return result.(model).result, nil
}

type tick time.Time

func (m model) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tick(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.selected = denyAction
		if m.tooSmall() {
			return m, nil
		}
		m.viewport.Width = m.width - 4
		m.feedback.SetWidth(m.viewport.Width)
		m.feedback.SetHeight(max(1, min(5, m.height-6-lipgloss.Height(m.feedbackFooter()))))
		var cmd tea.Cmd
		if m.editing {
			m.feedback, cmd = m.feedback.Update(msg)
		}
		m.viewport.SetContent(m.content())
		available := m.height - 5 - lipgloss.Height(m.footer(false))
		m.viewport.Height = min(m.viewport.TotalLineCount(), max(1, available))
		if m.scrollable() {
			m.viewport.Height = max(1, m.height-5-lipgloss.Height(m.footer(true)))
		}
		m.viewport.SetYOffset(m.viewport.YOffset)
		return m, cmd
	case tick:
		if !time.Now().Before(m.request.Deadline) {
			m.done = true
			return m, tea.Quit
		}
		return m, m.Init()
	case tea.KeyMsg:
		if msg.Alt {
			break
		}
		if msg.Type == tea.KeyCtrlC {
			m.done = true
			return m, tea.Quit
		}
		if m.editing {
			switch msg.Type {
			case tea.KeyEsc:
				m.editing = false
				m.selected = denyAction
				m.feedback.Blur()
				return m, nil
			case tea.KeyCtrlS:
				if m.tooSmall() {
					return m, nil
				}
				if time.Now().Before(m.request.Deadline) {
					if m.feedbackChars() > maxFeedbackChars {
						return m, nil
					}
					m.result.Feedback = strings.TrimSpace(m.feedback.Value())
				}
				m.done = true
				return m, tea.Quit
			}
			break
		}
		switch msg.Type {
		case tea.KeyRunes:
			if msg.Paste || len(msg.Runes) != 1 {
				return m, nil
			}
			if msg.Runes[0] == 'q' || msg.Runes[0] == 'n' {
				m.done = true
				return m, tea.Quit
			}
		case tea.KeyEsc:
			m.done = true
			return m, tea.Quit
		case tea.KeyTab, tea.KeyRight:
			m.selected = (m.selected + 1) % len(actionLabels)
			return m, nil
		case tea.KeyShiftTab, tea.KeyLeft:
			m.selected = (m.selected + len(actionLabels) - 1) % len(actionLabels)
			return m, nil
		case tea.KeyHome:
			m.viewport.GotoTop()
			return m, nil
		case tea.KeyEnd:
			m.viewport.GotoBottom()
			return m, nil
		case tea.KeyEnter:
			if !m.tooSmall() {
				if !time.Now().Before(m.request.Deadline) {
					m.done = true
					return m, tea.Quit
				}
				if m.selected == feedbackAction {
					m.editing = true
					return m, m.feedback.Focus()
				}
				m.result.Approved = m.selected == approveAction
				m.done = true
				return m, tea.Quit
			}
		}
	}
	var cmd tea.Cmd
	if m.editing {
		if !m.tooSmall() {
			if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyRunes && !key.Alt {
				// Literal text must not match the textarea's shortcut names.
				key.Paste = true
				msg = key
			}
			m.feedback, cmd = m.feedback.Update(msg)
		}
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
	}
	return m, cmd
}

func (m model) tooSmall() bool {
	return m.width < 36 || m.height < 12
}

func (m model) content() string {
	command := ansi.Hardwrap(Safe(m.request.Command), max(1, m.viewport.Width-2), true)
	sections := []string{
		commandStyle.Render(command),
		detailStyle.Render("Working directory: ") + Safe(m.request.Cwd),
	}
	patterns := make([]string, len(m.request.Rules))
	for i, pattern := range m.request.Rules {
		data, _ := json.Marshal(pattern)
		patterns[i] = string(data)
	}
	var details []string
	if len(patterns) > 0 {
		details = append(details, "Details: matched "+Safe(strings.Join(patterns, "\n")))
	}
	if m.request.SourcePane != "" {
		details = append(details, "Source pane: "+Safe(m.request.SourcePane))
	}
	if len(details) > 0 {
		sections = append(sections, detailStyle.Render(strings.Join(details, "\n")))
	}
	return ansi.Hardwrap(strings.Join(sections, "\n\n"), m.viewport.Width, true)
}

func (m model) scrollable() bool {
	return m.viewport.TotalLineCount() > m.viewport.Height
}

func (m model) footer(scrolling bool) string {
	var rows []string
	row := ""
	for i, label := range actionLabels {
		button := buttonStyle.Render("  " + label)
		if m.selected == i {
			button = selectedButtonStyle.Render("▶ " + label)
		}
		if row != "" && ansi.StringWidth(row)+2+ansi.StringWidth(button) > m.viewport.Width {
			rows = append(rows, row)
			row = ""
		}
		if row != "" {
			row += "  "
		}
		row += button
	}
	rows = append(rows, row)
	help := "Tab: select · Enter: confirm · Esc: deny"
	status := m.countdown()
	if scrolling {
		status = "↑/↓ PgUp/PgDn: scroll\n" + status
	}
	return strings.Join(rows, "\n") + "\n" +
		detailStyle.Render(ansi.Wrap(help, m.viewport.Width, "")) + "\n" +
		detailStyle.Render(ansi.Wrap(status, m.viewport.Width, ""))
}

func (m model) countdown() string {
	return fmt.Sprintf("Expires in %ds", max(0, int(time.Until(m.request.Deadline).Seconds())))
}

func (m model) feedbackFooter() string {
	help := "Enter: newline · Esc: back\nCtrl+C: deny without feedback"
	submit := "Ctrl+S: send feedback and deny"
	if m.feedbackChars() > maxFeedbackChars {
		submit = "Shorten feedback to send"
	}
	count := fmt.Sprintf("%d/%d chars", m.feedbackChars(), maxFeedbackChars)
	return detailStyle.Render(count) + "\n" + headingStyle.Render(submit) + "\n" +
		detailStyle.Render(ansi.Wrap(help, m.viewport.Width, "")) + "\n" +
		detailStyle.Render(m.countdown())
}

func (m model) feedbackChars() int {
	return utf8.RuneCountInString(m.feedback.Value())
}

func (m model) View() string {
	if m.done {
		return ""
	}
	if m.tooSmall() {
		if m.editing {
			return "Enlarge the terminal to edit.\nCtrl+C: deny without feedback"
		}
		return "Enlarge the terminal to review.\nEscape: deny"
	}
	if m.editing {
		content := headingStyle.Render("Deny with feedback") + "\n\n" +
			"What should Codex change?\n" + m.feedback.View() + "\n\n" + m.feedbackFooter()
		return lipgloss.NewStyle().Padding(1, 2).Render(content)
	}
	content := headingStyle.Render("Run this command?") + "\n\n" + m.viewport.View() + "\n\n" + m.footer(m.scrollable())
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

func Safe(s string) string {
	var out strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			out.WriteRune(r)
		case r == '\t':
			out.WriteString(`\t`)
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp):
			fmt.Fprintf(&out, `\u{%04X}`, r)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
